package console

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed assets
var assetFiles embed.FS

// view is a page of the console that its navigation lists.
type view struct {
	// Path is where the view is served; Title names it in the navigation
	// and the page title.
	Path, Title string
	// Fragment, if set, is the path under /api/ that serves the view's
	// refreshing region, its template "region", which the page marks
	// with data-refresh (ADR 0020).
	Fragment string
	// template renders the view's content inside the layout.
	template *template.Template
	// content returns what the template shows.
	content func(*http.Request) any
}

// views returns the console's views in navigation order. The navigation
// lists exactly these, so a view of the epic appears once it is built and
// added here (ADR 0019); the overview links to it from then on (ADR 0020).
func (c *Console) views() []view {
	return []view{
		{Path: "/", Title: "Overview", Fragment: "/api/overview", template: overviewTemplate, content: c.overviewContent},
	}
}

// Page templates: each is the shared layout with the page's content.
var (
	overviewTemplate = pageTemplate("overview.html")
	notFoundTemplate = pageTemplate("notfound.html")
)

func pageTemplate(file string) *template.Template {
	return template.Must(template.New(file).ParseFS(templateFiles, "templates/layout.html", "templates/"+file)).Lookup("layout")
}

// layoutPage is the data of the shared layout.
type layoutPage struct {
	Title     string
	Nav       []navItem
	Node      nodeSummary
	Mode      string
	ModeLabel string
	Health    Health
	// Content is the data of the page's own template.
	Content any
}

type navItem struct {
	Path, Title string
	Current     bool
}

type nodeSummary struct {
	PeerID, ShortPeerID, Version string
}

// modeLabels name node.mode.
var modeLabels = map[string]string{"observe": "Observe", "enforce": "Enforce"}

// layout returns the layout data for a page titled title whose navigation
// marks current.
func (c *Console) layout(title, current string, content any) layoutPage {
	mode := c.node.Mode()
	label, ok := modeLabels[mode]
	if !ok {
		label = mode
	}
	p := layoutPage{
		Title:     title,
		Node:      nodeSummary{PeerID: c.node.PeerID, ShortPeerID: shortPeerID(c.node.PeerID), Version: c.node.Version},
		Mode:      mode,
		ModeLabel: label,
		Health:    nodeHealth(c.node.Status()),
		Content:   content,
	}
	for _, v := range c.pages {
		p.Nav = append(p.Nav, navItem{Path: v.Path, Title: v.Title, Current: v.Path == current})
	}
	return p
}

// shortPeerID abbreviates a peer ID for the top bar.
func shortPeerID(id string) string {
	if len(id) <= 16 {
		return id
	}
	return id[:8] + "…" + id[len(id)-6:]
}

// serveView renders the view v.
func (c *Console) serveView(v view) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.render(w, http.StatusOK, v.template, c.layout(v.Title, v.Path, v.content(r)))
	})
}

// serveFragment renders the refreshing region of the view v alone, for
// the script that swaps it into the open page (ADR 0020).
func (c *Console) serveFragment(v view) http.Handler {
	region := v.template.Lookup("region")
	if region == nil {
		panic("console view " + v.Path + " declares a fragment but has no region template")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.render(w, http.StatusOK, region, v.content(r))
	})
}

// notFound renders the page for a path the console does not serve.
func (c *Console) notFound(w http.ResponseWriter, r *http.Request) {
	c.render(w, http.StatusNotFound, notFoundTemplate, c.layout("Page not found", "", struct{ Path string }{r.URL.Path}))
}

// overviewContent reads the node and returns the data of the overview.
func (c *Console) overviewContent(*http.Request) any {
	var facts Facts
	if c.node.Facts != nil {
		facts = c.node.Facts()
	}
	return buildOverview(overviewInput{now: c.now(), node: c.node, mode: c.node.Mode(), statuses: c.node.Status(),
		facts: facts, link: c.detailLink})
}

// detailLink returns path as the link to the view that details a number
// if the console has a view there (the query aside), and otherwise the
// obiectl command that shows the same, so the overview never links to a
// page that does not exist (ADR 0020).
func (c *Console) detailLink(path, command string) (href, cmd string) {
	route, _, _ := strings.Cut(path, "?")
	for _, v := range c.pages {
		if v.Path == route {
			return path, ""
		}
	}
	return "", command
}

// assets serves the embedded stylesheet, script and icon. They hold no
// data about the node, so the sign-in page may load them.
func assets() http.Handler {
	sub, err := fs.Sub(assetFiles, "assets")
	if err != nil {
		panic(err) // the embedded directory exists
	}
	files := http.StripPrefix("/assets/", http.FileServerFS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r) // no directory listings
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	})
}
