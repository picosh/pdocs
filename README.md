# pdocs

A static site generator for pico docs

# usage

```go
func main() {
  pager := pdocs.Pager("./docs")
	sitemap := *pdocs.Sitemap {
    Children: []*pdocs.Sitemap{
      {Text: "Home", Href: "/", Page: pager("home.md")},
      {
        Text: "How it Works",
        Href: "/how-it-works",
        Page: pager("how-it-works.md"),
        Children: []*Sitemap{
          {Text: "Port Forward"},
          {Text: "Traditional VPN"},
          {Text: "sish Public"},
          {Text: "sish Private"},
          {Text: "Additional Details"},
        },
      },
      {
        Text: "Getting Started",
        Href: "/getting-started",
        Page: pager("getting-started.md"),
        Children: []*Sitemap{
          {Text: "Managed"},
          {Text: "Docker"},
          {Text: "Docker Compose"},
          {Text: "Google Cloud Platform"},
          {Text: "Authentication"},
          {Text: "DNS Setup"},
        },
      },
      {
        Text: "Forwarding Types",
        Href: "/forwarding-types",
        Page: pager("forwarding-types.md"),
        Children: []*Sitemap{
          {Text: "HTTP"},
          {Text: "TCP"},
          {Text: "TCP Alias"},
          {Text: "SNI"},
        },
      },
      {Text: "CLI", Href: "/cli", Page: pager("cli.md")},
      {
        Text: "Advanced",
        Href: "/advanced",
        Page: pager("advanced.md"),
        Children: []*Sitemap{
          {Text: "Allowlist IPs"},
          {Text: "Custom Domains"},
          {Text: "Load Balancing"},
        },
      },
      {Text: "FAQ", Href: "/faq", Page: pager("faq.md")},
    },
  }

	config := &pdocs.DocConfig{
		Sitemap:  sitemap,
		Out:      "./public",
		Tmpl:     "./tmpl",
    PageTmpl: "post.page.tmpl",
	}

	err := config.GenSite()
	if err != nil {
		panic(err)
	}
}
```
