package pdocs

import (
	"bytes"
	"fmt"
	"html/template"
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
}

type ParsedText struct {
	Html template.HTML
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

func AstToc(doc ast.Node, src []byte, mtoc int) error {
	var tree *toc.TOC
	if mtoc >= 0 {
		var err error
		if mtoc > 0 {
			tree, err = toc.Inspect(doc, src, toc.Compact(true), toc.MinDepth(0), toc.MaxDepth(mtoc))
		} else {
			tree, err = toc.Inspect(doc, src, toc.Compact(true), toc.MinDepth(0))
		}
		if err != nil {
			return err
		}
		if tree == nil {
			return nil // no headings?
		}
	}
	list := toc.RenderList(tree)
	if list == nil {
		return nil // no headings
	}

	list.SetAttributeString("id", []byte("toc-list"))

	// generate # toc
	heading := ast.NewHeading(2)
	heading.SetAttributeString("id", []byte("toc"))
	heading.AppendChild(heading, ast.NewString([]byte("Table of Contents")))

	// insert
	doc.InsertBefore(doc, doc.FirstChild(), list)
	doc.InsertBefore(doc, doc.FirstChild(), heading)
	return nil
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
			Position: anchor.Before,
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
	if mtoc >= 0 {
		err = AstToc(doc, btext, mtoc)
		if err != nil {
			return &parsed, fmt.Errorf("error generating toc: %w", err)
		}
	}

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

	return &parsed, nil
}

func walkDir(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			fmt.Println(path)
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
	Page         string
	Href         string
	Data         *ParsedText
	Prev         *Sitemap
	Next         *Sitemap
	Cur          *Sitemap
	Sitemap      *Sitemap
	SitemapByTag map[string][]*Sitemap
}

type DocConfig struct {
	Out      string
	Tmpl     string
	Sitemap  *Sitemap
	PageTmpl string
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

func walkSitemap(sitemap *Sitemap, mapper map[string][]*Sitemap) {
	for _, toc := range sitemap.Children {
		if toc.Tag == "" {
			continue
		}
		if toc.Href != "" {
			mapper[toc.Tag] = append(mapper[toc.Tag], toc)
		}
		walkSitemap(toc, mapper)
	}
}

func groupByTag(sitemap *Sitemap) map[string][]*Sitemap {
	mapper := map[string][]*Sitemap{}
	walkSitemap(sitemap, mapper)
	return mapper
}

func (config *DocConfig) GenSite() error {
	tmpl, err := walkDir(config.Tmpl)
	if err != nil {
		return err
	}

	for _, toc := range config.Sitemap.Children {
		for idx := range toc.Children {
			toc.Children[idx].ParentHref = toc.Href
		}
	}

	sitemapByTag := groupByTag(config.Sitemap)

	for idx, toc := range config.Sitemap.Children {
		if toc.Page == "" {
			continue
		}

		data, err := os.ReadFile(toc.Page)
		if err != nil {
			return err
		}
		d, err := parseMarkdown(string(data))
		if err != nil {
			return err
		}

		var prev *Sitemap
		if idx > 0 {
			prev = config.Sitemap.Children[idx-1]
		}
		var next *Sitemap
		if idx+1 < len(config.Sitemap.Children) {
			next = config.Sitemap.Children[idx+1]
		}

		err = config.GenPage(tmpl, &Page{
			Page:         toc.Page,
			Href:         toc.Href,
			Data:         d,
			Cur:          toc,
			Prev:         prev,
			Next:         next,
			Sitemap:      config.Sitemap,
			SitemapByTag: sitemapByTag,
		})
		if err != nil {
			return err
		}

	}

	return nil
}

type Sitemap struct {
	ParentHref string
	Text       string
	Href       string
	Page       string
	Tag        string
	Hidden     bool
	Data       any
	Children   []*Sitemap
}

func (sitemap *Sitemap) Slug() string {
	return strings.ReplaceAll(
		strings.ToLower(sitemap.Text),
		" ",
		"-",
	)
}

func (sitemap *Sitemap) GenHref() template.HTML {
	if sitemap.Href == "" {
		return template.HTML(fmt.Sprintf("%s#%s", sitemap.ParentHref, sitemap.Slug()))
	}
	return template.HTML(sitemap.Href)
}
