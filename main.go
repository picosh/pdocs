package pdocs

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	gtext "github.com/yuin/goldmark/text"
	"go.abhg.dev/goldmark/anchor"
	"go.abhg.dev/goldmark/toc"
)

type MetaData struct {
	Title       string
	Description string
	Slug        string
	Keywords    []string
	Template    string
	Toc         *toc.TOC
}

type ParsedText struct {
	Html    template.HTML
	TocHtml template.HTML
	*MetaData
}

func toString(obj interface{}) string {
	if obj == nil {
		return ""
	}
	return obj.(string)
}

func toKeywords(obj interface{}) ([]string, error) {
	arr := make([]string, 0)
	if obj == nil {
		return arr, nil
	}

	switch raw := obj.(type) {
	case []interface{}:
		for _, tag := range raw {
			arr = append(arr, tag.(string))
		}
	case string:
		keywords := strings.Split(raw, " ")
		for _, tag := range keywords {
			arr = append(arr, strings.TrimSpace(tag))
		}
	default:
		return arr, fmt.Errorf("unsupported type for `keywords` variable: %T", raw)
	}

	return arr, nil
}

// The toc frontmatter can take a boolean or an integer.
//
// A value of -1 or false means "do not generate a toc".
// A value of 0 or true means "generate a toc with no depth limit".
// A value of >0 means "generate a toc with a depth limit of $value past title".
func toToc(obj interface{}) (int, error) {
	if obj == nil {
		return -1, nil
	}
	switch val := obj.(type) {
	case bool:
		if val {
			return 0, nil
		}
		return -1, nil
	case int:
		if val < -1 {
			val = -1
		}
		return val, nil
	default:
		return -1, fmt.Errorf("incorrect type for value: %T, should be bool or int", val)
	}
}

func AstToc(doc ast.Node, src []byte, mtoc int) (*toc.TOC, ast.Node, error) {
	var tree *toc.TOC
	var err error
	if mtoc > 0 {
		tree, err = toc.Inspect(doc, src, toc.Compact(true), toc.MinDepth(0), toc.MaxDepth(mtoc))
	} else {
		tree, err = toc.Inspect(doc, src, toc.Compact(true), toc.MinDepth(0))
	}
	if err != nil {
		return nil, nil, err
	}
	if tree == nil {
		return nil, nil, nil // no headings?
	}
	list := toc.RenderList(tree)
	if list == nil {
		return tree, nil, nil // no headings
	}

	list.SetAttributeString("id", []byte("toc-list"))

	if mtoc >= 0 {
		// insert
		return tree, list, nil
	}
	return tree, nil, nil
}

func CreateGoldmark(extenders ...goldmark.Extender) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(
			extenders...,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
		),
	)
}

func parseMarkdown(text string) (*ParsedText, error) {
	parsed := ParsedText{
		MetaData: &MetaData{
			Keywords: []string{},
		},
	}
	hili := highlighting.NewHighlighting(
		highlighting.WithFormatOptions(
			html.WithLineNumbers(true),
			html.WithClasses(true),
		),
	)
	extenders := []goldmark.Extender{
		extension.GFM,
		extension.Footnote,
		meta.Meta,
		hili,
		&anchor.Extender{
			Position: anchor.After,
			Texter:   anchor.Text("#"),
		},
	}
	md := CreateGoldmark(extenders...)
	context := parser.NewContext()
	// we do the Parse/Render steps manually to get a chance to examine the AST
	btext := []byte(text)
	doc := md.Parser().Parse(gtext.NewReader(btext), parser.WithContext(context))
	metaData := meta.Get(context)

	parsed.Title = toString(metaData["title"])
	parsed.Description = toString(metaData["description"])
	parsed.Slug = toString(metaData["slug"])
	parsed.Template = toString(metaData["template"])
	mtoc, err := toToc(metaData["toc"])
	if err != nil {
		return &parsed, fmt.Errorf("front-matter field (%s): %w", "toc", err)
	}
	tree, listNode, err := AstToc(doc, btext, mtoc)
	if err != nil {
		return &parsed, fmt.Errorf("error generating toc: %w", err)
	}
	parsed.Toc = tree

	keywords, err := toKeywords(metaData["keywords"])
	if err != nil {
		return &parsed, err
	}
	parsed.Keywords = keywords

	// Rendering happens last to allow any of the previous steps to manipulate
	// the AST.
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, btext, doc); err != nil {
		return &parsed, err
	}
	parsed.Html = template.HTML(buf.String())

	if listNode != nil {
		var tocbuf bytes.Buffer
		ttext := []byte{}
		if err := md.Renderer().Render(&tocbuf, ttext, listNode); err != nil {
			return &parsed, err
		}
		parsed.TocHtml = template.HTML(tocbuf.String())
	}

	return &parsed, nil
}

func walkDir(logger *slog.Logger, root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			logger.Info("found template", "path", path)
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

func renderTemplate(templates []string, tmpl string) (*template.Template, error) {
	files := make([]string, len(templates))
	copy(files, templates)
	files = append(
		files,
		tmpl,
	)

	ts, err := template.New("base").ParseFiles(files...)
	if err != nil {
		return nil, err
	}
	return ts, nil
}

func Pager(dir string) func(p string) string {
	return func(p string) string {
		return filepath.Join(dir, p)
	}
}

func AnchorTagUrl(title string) string {
	lower := strings.ToLower(title)
	hyphen := strings.ReplaceAll(lower, " ", "-")
	period := strings.ReplaceAll(hyphen, ".", "")
	tick := strings.ReplaceAll(period, "`", "")
	return fmt.Sprintf("#%s", tick)
}

func AnchorTagSitemap(title string) *Sitemap {
	return &Sitemap{
		Text: title,
		Href: AnchorTagUrl(title),
	}
}

// ============================================

type Page struct {
	Page    string
	Href    string
	Data    *ParsedText
	Prev    *Sitemap
	Next    *Sitemap
	Cur     *Sitemap
	Sitemap *Sitemap
	CacheId string
}

type DocConfig struct {
	Out      string
	Tmpl     string
	Sitemap  *Sitemap
	PageTmpl string
	Logger   *slog.Logger
	CacheId  string
}

func (config *DocConfig) GenPage(templates []string, page *Page) error {
	tmpl := config.PageTmpl
	if page.Data.Template != "" {
		tmpl = page.Data.Template
	}

	ts, err := renderTemplate(templates, filepath.Join(config.Tmpl, tmpl))

	if err != nil {
		return err
	}

	buf := new(bytes.Buffer)
	err = ts.Execute(buf, page)
	if err != nil {
		return err
	}

	base := filepath.Base(strings.ReplaceAll(page.Page, ".md", ""))
	name := base
	if page.Data.Slug != "" {
		name = page.Data.Slug
	}

	if _, err := os.Stat(config.Out); os.IsNotExist(err) {
		err := os.MkdirAll(config.Out, 0755)
		if err != nil {
			return err
		}
	}

	fp := filepath.Join(
		config.Out,
		fmt.Sprintf("%s.html", name),
	)
	err = os.WriteFile(fp, buf.Bytes(), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (config *DocConfig) toSitemap(href string, item *toc.Item) *Sitemap {
	sm := Sitemap{
		Text: string(item.Title),
		Href: fmt.Sprintf("%s#%s", href, string(item.ID)),
	}
	for _, itm := range item.Items {
		nextsm := config.toSitemap(href, itm)
		sm.Children = append(sm.Children, nextsm)
	}
	return &sm
}

func (config *DocConfig) genSitemap(pg *PageComp, node *Sitemap, tmpl []string) error {
	for _, toc := range node.Children {
		if toc.Page != "" {
			data, err := os.ReadFile(toc.Page)
			if err != nil {
				return err
			}
			d, err := parseMarkdown(string(data))
			if err != nil {
				return err
			}

			for _, item := range d.Toc.Items {
				sm := config.toSitemap(toc.Href, item)
				toc.Children = append(toc.Children, sm)
			}

			pg.Pages = append(pg.Pages, &Page{
				Page:    toc.Page,
				Href:    toc.Href,
				Data:    d,
				Cur:     toc,
				Sitemap: config.Sitemap,
				CacheId: config.CacheId,
			})
		}

		if len(toc.Children) > 0 {
			err := config.genSitemap(pg, toc, tmpl)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

type PageComp struct {
	Pages []*Page
}

func (config *DocConfig) GenSite() error {
	tmpl, err := walkDir(config.Logger, config.Tmpl)
	if err != nil {
		return err
	}

	pg := &PageComp{}
	err = config.genSitemap(pg, config.Sitemap, tmpl)
	if err != nil {
		return err
	}
	for idx, page := range pg.Pages {
		config.Logger.Info(
			"generating page",
			"page", page.Page,
			"href", page.Href,
		)
		var prev *Sitemap
		if idx > 0 {
			prev = pg.Pages[idx-1].Cur
		}
		var next *Sitemap
		if idx+1 < len(pg.Pages) {
			next = pg.Pages[idx+1].Cur
		}
		page.Prev = prev
		page.Next = next
		err := config.GenPage(tmpl, page)
		if err != nil {
			return err
		}
	}
	return nil
}

type Sitemap struct {
	Text     string
	Href     string
	Page     string
	Hidden   bool
	Data     any
	Children []*Sitemap
}

func (sitemap *Sitemap) Slug() string {
	return strings.ReplaceAll(
		strings.ToLower(sitemap.Text),
		" ",
		"-",
	)
}

func (sitemap *Sitemap) GenHref() template.HTML {
	return template.HTML(sitemap.Href)
}
