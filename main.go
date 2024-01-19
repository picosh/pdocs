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
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"go.abhg.dev/goldmark/anchor"
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

func parseMarkdown(text string) (*ParsedText, error) {
	parsed := ParsedText{
		MetaData: &MetaData{
			Keywords: []string{},
		},
	}
	var buf bytes.Buffer
	hili := highlighting.NewHighlighting(
		highlighting.WithFormatOptions(
			html.WithLineNumbers(true),
			html.WithClasses(true),
		),
	)
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			meta.Meta,
			hili,
			&anchor.Extender{
				Position: anchor.Before,
				Texter:   anchor.Text("#"),
			},
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(
			ghtml.WithUnsafe(),
		),
	)
	context := parser.NewContext()
	if err := md.Convert([]byte(text), &buf, parser.WithContext(context)); err != nil {
		return &parsed, err
	}
	parsed.Html = template.HTML(buf.String())

	metaData := meta.Get(context)
	parsed.Title = toString(metaData["title"])
	parsed.Description = toString(metaData["description"])
	parsed.Slug = toString(metaData["slug"])
	parsed.Template = toString(metaData["template"])

	keywords, err := toKeywords(metaData["keywords"])
	if err != nil {
		return &parsed, err
	}
	parsed.Keywords = keywords

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

// ============================================

type Page struct {
	Page    string
	Href string
	Data    *ParsedText
	Prev    *Sitemap
	Next    *Sitemap
	Sitemap []*Sitemap
}

type DocConfig struct {
	Out      string
	Tmpl     string
	Sitemap  []*Sitemap
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

func (config *DocConfig) GenSite() error {
	tmpl, err := walkDir(config.Tmpl)
	if err != nil {
		return err
	}

	for _, toc := range config.Sitemap {
		for idx := range toc.Children {
			toc.Children[idx].ParentHref = toc.Href
		}
	}

	for idx, toc := range config.Sitemap {
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
			prev = config.Sitemap[idx-1]
		}
		var next *Sitemap
		if idx+1 < len(config.Sitemap) {
			next = config.Sitemap[idx+1]
		}

		err = config.GenPage(tmpl, &Page{
			Page:    toc.Page,
			Href: 	 toc.Href,
			Data:    d,
			Prev:    prev,
			Next:    next,
			Sitemap: config.Sitemap,
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
