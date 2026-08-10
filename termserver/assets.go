package termserver

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed assets
var embeddedAssets embed.FS

var (
	indexTemplate = template.Must(template.ParseFS(embeddedAssets, "assets/index.html"))
	staticAssets  = mustSubFS(embeddedAssets, "assets")
)

type indexData struct {
	TokenMetaName string
	Token         string
	CSRFMetaName  string
	CSRFToken     string
}

func mustSubFS(source fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(source, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func (s *Server) renderIndex(w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return indexTemplate.Execute(w, indexData{
		TokenMetaName: tokenMetaName,
		Token:         s.token,
		CSRFMetaName:  csrfMetaName,
		CSRFToken:     s.csrfToken,
	})
}

func staticHandler() http.Handler {
	return http.FileServer(http.FS(staticAssets))
}
