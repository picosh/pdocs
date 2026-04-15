package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"io"
	"os"
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

func renderTemplate(templates []string, tmpl string) (*template.Template, error) {
	files := make([]string, len(templates))
	copy(files, templates)
	files = append(files, tmpl)

	ts, err := template.New("base").ParseFiles(files...)
	if err != nil {
		return nil, err
	}
	return ts, nil
}

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

func main() {
	tmplPaths := flag.String("tmpl", "", "Comma-separated list of template file paths (required)")
	forceTOC := flag.Bool("toc", false, "Force generate table of contents")
	flag.Parse()

	if *tmplPaths == "" {
		fmt.Fprintln(os.Stderr, "Error: -tmpl flag is required")
		flag.Usage()
		os.Exit(1)
	}

	// Split comma-separated template paths
	templatePaths := strings.Split(*tmplPaths, ",")
	for i := range templatePaths {
		templatePaths[i] = strings.TrimSpace(templatePaths[i])
	}

	// Read markdown from stdin
	markdown, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	// Parse markdown
	parsed, err := parseMarkdown(string(markdown))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing markdown: %v\n", err)
		os.Exit(1)
	}

	// If -toc flag is set, ensure TOC is generated
	if *forceTOC && parsed.Toc != nil && parsed.TocHtml == "" {
		var tocbuf bytes.Buffer
		ttext := []byte{}
		md := CreateGoldmark(
			extension.GFM,
			extension.Footnote,
			highlighting.NewHighlighting(
				highlighting.WithFormatOptions(
					html.WithLineNumbers(true),
					html.WithClasses(true),
				),
			),
		)
		list := toc.RenderList(parsed.Toc)
		if list != nil {
			list.SetAttributeString("id", []byte("toc-list"))
			if err := md.Renderer().Render(&tocbuf, ttext, list); err != nil {
				fmt.Fprintf(os.Stderr, "Error rendering TOC: %v\n", err)
				os.Exit(1)
			}
			parsed.TocHtml = template.HTML(tocbuf.String())
		}
	}

	// Load template files
	templateFiles := make([]string, len(templatePaths))
	copy(templateFiles, templatePaths)

	ts, err := renderTemplate(templateFiles, templatePaths[len(templatePaths)-1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error rendering template: %v\n", err)
		os.Exit(1)
	}

	// Create page struct
	baseName := "index"
	if parsed.Slug != "" {
		baseName = parsed.Slug
	}

	page := &Page{
		Page:    "stdin",
		Href:    "/" + baseName + ".html",
		Data:    parsed,
		Cur:     &Sitemap{Text: parsed.Title, Href: "/" + baseName + ".html"},
		Sitemap: nil,
	}

	// Execute template
	buf := new(bytes.Buffer)
	err = ts.Execute(buf, page)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error executing template: %v\n", err)
		os.Exit(1)
	}

	// Write to stdout
	os.Stdout.Write(buf.Bytes())
}

