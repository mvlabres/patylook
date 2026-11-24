package web

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"patylook/internal/core/domain"
)

// pageData is the root object every template receives.
type pageData struct {
	Title  string
	Active string // navigation key of the current section
	User   *domain.User
	CSRF   string
	Flash  *domain.Flash
	Errors map[string]string // field errors; the "" key holds form-level messages
	Data   map[string]any
}

type templates struct {
	pages map[string]*template.Template
}

func templateFuncs(loc *time.Location) template.FuncMap {
	return template.FuncMap{
		"money": func(m domain.Money) string { return m.String() },
		"moneyPtr": func(m *domain.Money) string {
			if m == nil {
				return "—"
			}
			return m.String()
		},
		"moneyInput":   func(m domain.Money) string { return m.Input() },
		"percent":      func(p domain.Percent) string { return p.String() },
		"percentInput": func(p domain.Percent) string { return p.Input() },
		"date": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("02/01/2006")
		},
		"datePtr": func(t *time.Time) string {
			if t == nil {
				return "—"
			}
			return t.Format("02/01/2006")
		},
		"dateInput": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"datePtrInput": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"datetime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.In(loc).Format("02/01/2006 15:04")
		},
		"cpf":         formatCPF,
		"statusLabel": func(s domain.OrderStatus) string { return s.Label() },
		"add":         func(a, b int) int { return a + b },
		"sub":         func(a, b int) int { return a - b },
		"lineTotal":   func(i domain.OrderItem) domain.Money { return i.Total() },
		"dict":        dict,
	}
}

// dict builds a map from alternating key/value arguments, for passing several values to a partial.
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, fmt.Errorf("dict: número ímpar de argumentos")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: chave %v não é string", kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

func formatCPF(s string) string {
	if len(s) != 11 {
		return s
	}
	return s[:3] + "." + s[3:6] + "." + s[6:9] + "-" + s[9:]
}

// loadTemplates parses every page together with the layout and partials.
// Each page gets its own template set so block names cannot collide.
func loadTemplates(fsys fs.FS, loc *time.Location) (*templates, error) {
	pageFiles, err := fs.Glob(fsys, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}
	if len(pageFiles) == 0 {
		return nil, fmt.Errorf("nenhuma página encontrada em templates/pages")
	}
	t := &templates{pages: make(map[string]*template.Template, len(pageFiles))}
	for _, file := range pageFiles {
		name := strings.TrimSuffix(path.Base(file), ".html")
		set, err := template.New(name).
			Funcs(templateFuncs(loc)).
			Option("missingkey=error").
			ParseFS(fsys, "templates/layout.html", "templates/partials/*.html", file)
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", name, err)
		}
		t.pages[name] = set
	}
	return t, nil
}

// render executes into a buffer first so a template error does not leave a half-written response.
func (t *templates) render(w http.ResponseWriter, status int, page string, data pageData) error {
	set, ok := t.pages[page]
	if !ok {
		return fmt.Errorf("página %q não existe", page)
	}
	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, "layout", data); err != nil {
		return fmt.Errorf("renderizar %s: %w", page, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}
