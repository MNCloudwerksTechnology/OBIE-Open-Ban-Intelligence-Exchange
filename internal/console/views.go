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
	// region, if set, returns what the refreshing region shows, when that
	// is cheaper to read than content (ADR 0022); else the region gets
	// content.
	region func(*http.Request) any
	// item, if set, serves the pages of the view's items (e.g. one peer)
	// at Path + "/{id}", marked as the view in the navigation, and their
	// refreshing regions at Fragment + "/{id}" (ADR 0021).
	item *item
}

// item is the page of one item of a view.
type item struct {
	// template renders the page's content inside the layout; its template
	// "region" renders the refreshing region.
	template *template.Template
	// content returns the page's title and data for the item r names
	// ({id}), and whether the console knows the item. With region set,
	// only the data of the refreshing region is needed; it is rendered
	// also for an item the console no longer knows, so an open page can
	// say so.
	content func(r *http.Request, region bool) (title string, data any, ok bool)
	// missing explains that the console knows no such item.
	missing string
	// rest makes the ID the rest of the path, so it may hold a slash, as
	// a network does (ADR 0022).
	rest bool
}

// pattern returns the route pattern of the items below path.
func (it *item) pattern(path string) string {
	if it.rest {
		return path + "/{id...}"
	}
	return path + "/{id}"
}

// views returns the console's views in navigation order. The navigation
// lists exactly these, so a view of the epic appears once it is built and
// added here (ADR 0019); the overview links to it from then on (ADR 0020).
func (c *Console) views() []view {
	return []view{
		{Path: "/", Title: "Overview", Fragment: "/api/overview", template: overviewTemplate, content: c.overviewContent},
		{Path: "/peers", Title: "Peers", Fragment: "/api/peers", template: peersTemplate, content: c.peersContent,
			item: &item{template: peerTemplate, content: c.peerContent,
				missing: "This node knows no such peer: it is neither configured nor connected, and the node holds no verdict of it."}},
		{Path: "/decisions", Title: "Decisions", Fragment: "/api/decisions", template: decisionsTemplate,
			content: c.decisionsContent, region: c.decisionsStatusContent,
			item: &item{template: decisionTemplate, content: c.decisionContent, missing: missingDecision, rest: true}},
		{Path: "/enforcement", Title: "Firewall", Fragment: "/api/enforcement", template: firewallTemplate,
			content: c.firewallContent, region: c.firewallSummaryContent},
	}
}

// Page templates: each is the shared layout with the page's content.
var (
	overviewTemplate  = pageTemplate("overview.html")
	peersTemplate     = pageTemplate("peers.html")
	peerTemplate      = pageTemplate("peer.html")
	decisionsTemplate = pageTemplate("decisions.html")
	decisionTemplate  = pageTemplate("decision.html")
	firewallTemplate  = pageTemplate("firewall.html")
	notFoundTemplate  = pageTemplate("notfound.html")
)

func pageTemplate(file string) *template.Template {
	return template.Must(template.New(file).Funcs(templateFuncs).ParseFS(templateFiles, "templates/layout.html", "templates/"+file)).
		Lookup("layout")
}

// templateFuncs are the functions page templates may call.
var templateFuncs = template.FuncMap{
	// count formats a number with thousands separators.
	"count": count,
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
	// Current is the link's aria-current: "page" on the view itself,
	// "true" on the page of one of its items, empty elsewhere.
	Current string
}

type nodeSummary struct {
	PeerID, ShortPeerID, Version string
}

// modeLabels name node.mode.
var modeLabels = map[string]string{"observe": "Observe", "enforce": "Enforce"}

// layout returns the layout data for a page titled title at the path
// current, whose navigation marks the view current is, or belongs to.
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
		item := navItem{Path: v.Path, Title: v.Title}
		switch {
		case v.Path == current:
			item.Current = "page"
		case v.item != nil && strings.HasPrefix(current, v.Path+"/"):
			item.Current = "true"
		}
		p.Nav = append(p.Nav, item)
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
	content := v.content
	if v.region != nil {
		content = v.region
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.render(w, http.StatusOK, region, content(r))
	})
}

// serveItem renders the page of the item of view v that the request
// names.
func (c *Console) serveItem(v view) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		title, data, ok := v.item.content(r, false)
		if !ok {
			c.render(w, http.StatusNotFound, notFoundTemplate, c.layout("Page not found", "",
				notFoundPage{Path: r.URL.Path, Message: v.item.missing}))
			return
		}
		c.render(w, http.StatusOK, v.item.template, c.layout(title, r.URL.Path, data))
	})
}

// serveItemFragment renders the refreshing region of an item's page
// alone.
func (c *Console) serveItemFragment(v view) http.Handler {
	region := v.item.template.Lookup("region")
	if region == nil {
		panic("console view " + v.Path + " declares items with a fragment but their pages have no region template")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, data, _ := v.item.content(r, true)
		c.render(w, http.StatusOK, region, data)
	})
}

// notFoundPage is the data of the page for a path the console does not
// serve.
type notFoundPage struct {
	Path string
	// Message says more about what is missing; empty for an unknown path.
	Message string
}

// notFound renders the page for a path the console does not serve.
func (c *Console) notFound(w http.ResponseWriter, r *http.Request) {
	c.render(w, http.StatusNotFound, notFoundTemplate, c.layout("Page not found", "", notFoundPage{Path: r.URL.Path}))
}

// overviewContent reads the node and returns the data of the overview.
// The statuses are read first: a part that ran then also ran while its
// numbers were read, or is stopping by now.
func (c *Console) overviewContent(*http.Request) any {
	statuses := c.node.Status()
	var facts Facts
	if c.node.Facts != nil {
		facts = c.node.Facts()
	}
	return buildOverview(overviewInput{now: c.now(), node: c.node, mode: c.node.Mode(), statuses: statuses,
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
