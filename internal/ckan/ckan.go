// Package ckan es el cliente de la API CKAN de datos.gob.cl (package_search, resource_show, datastore_search).
package ckan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// BaseURL es la API de datos.gob.cl.
const BaseURL = "https://datos.gob.cl/api/action"

// FuenteCKAN: timeout y reintentos para datos.gob.cl.
var FuenteCKAN = httpx.Fuente{Nombre: "datos.gob.cl", Timeout: 15 * time.Second, Intentos: 3, Espera: 500 * time.Millisecond}

// Client llama a CKAN. Cachea los recursos del Registro de Empresas.
type Client struct {
	HTTP    *httpx.Client
	BaseURL string
	Fuente  httpx.Fuente

	empresaMu     sync.Mutex
	empresaCache  *EmpresaAnios
	empresaCacheT time.Time
}

// New crea un cliente contra datos.gob.cl.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, BaseURL: BaseURL, Fuente: FuenteCKAN}
}

// Error es un error devuelto por CKAN ({"success":false,"error":{...}}), ya traducido.
type Error struct {
	Tipo   string // "Not Found Error", "Validation Error", "Search Error", ...
	Msg    string // mensaje en español para el LLM
	Campos map[string]string
}

func (e *Error) Error() string { return e.Msg }

// NotFound dice si CKAN respondió "Not Found Error".
func (e *Error) NotFound() bool { return e.Tipo == "Not Found Error" }

// IsNotFound dice si err es un Error de CKAN "Not Found".
func IsNotFound(err error) bool {
	var ce *Error
	return errors.As(err, &ce) && ce.NotFound()
}

// nombres de parámetros de CKAN -> parámetros de las tools.
var ckanParam = map[string]string{"sort": "ordenar", "filters": "filtros", "fields": "campos", "q": "q", "limit": "limite", "offset": "offset", "resource_id": "resource_id"}

// ParseError traduce el cuerpo de error de CKAN a una frase útil. Devuelve nil si no es un error de CKAN.
func ParseError(body []byte) *Error {
	var r struct {
		Success *bool                      `json:"success"`
		Error   map[string]json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &r) != nil || r.Error == nil {
		return nil
	}
	e := &Error{Campos: map[string]string{}}
	_ = json.Unmarshal(r.Error["__type"], &e.Tipo)
	var raw string
	_ = json.Unmarshal(r.Error["message"], &raw)
	for k, v := range r.Error {
		if k == "__type" || k == "message" || k == "info" {
			continue
		}
		var l []string
		if json.Unmarshal(v, &l) == nil && len(l) > 0 {
			e.Campos[k] = strings.Join(l, "; ")
		}
	}
	switch {
	case e.Tipo == "Not Found Error":
		e.Msg = "No encontrado en datos.gob.cl: " + strings.TrimPrefix(raw, "No encontrado: ")
	case e.Tipo == "Search Error":
		e.Msg = "La búsqueda tiene sintaxis inválida (caracteres como : \" ( ) [ ] * son operadores). Probá con palabras simples."
	case e.Campos["query"] != "":
		e.Msg = "Consulta inválida para DataStore. Lo más común: un filtro numérico contra un campo de texto (los filtros van como string), o un campo de 'ordenar' mal escrito. Revisá campos y tipos con ver_recurso."
	case len(e.Campos) > 0:
		keys := make([]string, 0, len(e.Campos))
		for k := range e.Campos {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			name := k
			if p, ok := ckanParam[k]; ok {
				name = p
			}
			parts = append(parts, fmt.Sprintf("%s: %s", name, strings.ReplaceAll(e.Campos[k], "invalid value", "valor inválido")))
		}
		e.Msg = "Parámetro inválido — " + strings.Join(parts, "; ") + ". Revisá los nombres exactos de los campos con ver_recurso (distinguen mayúsculas y tildes)."
	case raw != "":
		e.Msg = "datos.gob.cl respondió con error: " + raw
	default:
		e.Msg = "datos.gob.cl respondió con un error (" + e.Tipo + ")"
	}
	return e
}

// Get llama a una acción de la API CKAN y traduce sus errores.
func (c *Client) Get(ctx context.Context, action string, params url.Values) ([]byte, error) {
	body, err := c.HTTP.Get(ctx, c.Fuente, c.BaseURL+"/"+action+"?"+params.Encode())
	var he *httpx.HTTPError
	if errors.As(err, &he) {
		if ce := ParseError(he.Body); ce != nil {
			return nil, ce
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if ce := ParseError(body); ce != nil {
		return nil, ce
	}
	return body, nil
}

// getJSON llama a la acción y decodifica la respuesta en v.
func (c *Client) getJSON(ctx context.Context, action string, params url.Values, v any) error {
	body, err := c.Get(ctx, action, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("respuesta inesperada de datos.gob.cl: %v", err)
	}
	return nil
}

// Resource es un recurso (archivo) de un dataset.
type Resource struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Format          string `json:"format"`
	URL             string `json:"url"`
	DatastoreActive bool   `json:"datastore_active"`
	PackageID       string `json:"package_id"`
}

// Dataset es un resultado de package_search.
type Dataset struct {
	Title        string `json:"title"`
	Name         string `json:"name"`
	Notes        string `json:"notes"`
	Organization struct {
		Name  string `json:"name"`
		Title string `json:"title"`
	} `json:"organization"`
	Resources []Resource `json:"resources"`
}

// SearchResult es la respuesta de package_search.
type SearchResult struct {
	Count   int       `json:"count"`
	Results []Dataset `json:"results"`
}

// PackageSearch busca datasets.
func (c *Client) PackageSearch(ctx context.Context, params url.Values) (*SearchResult, error) {
	var resp struct {
		Result SearchResult `json:"result"`
	}
	if err := c.getJSON(ctx, "package_search", params, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

// FacetItem es un valor de una faceta (organismo o tag) con su cantidad de datasets.
type FacetItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Count       int    `json:"count"`
}

// Facets es el total de datasets y sus facetas por campo.
type Facets struct {
	Count        int `json:"count"`
	SearchFacets map[string]struct {
		Items []FacetItem `json:"items"`
	} `json:"search_facets"`
}

// Items devuelve los valores de una faceta.
func (f *Facets) Items(field string) []FacetItem { return f.SearchFacets[field].Items }

// PackageFacets pide las facetas de todos los datasets.
func (c *Client) PackageFacets(ctx context.Context, fields string, limit int) (*Facets, error) {
	params := url.Values{
		"q":           {"*:*"},
		"rows":        {"0"},
		"facet.field": {fields},
		"facet.limit": {fmt.Sprint(limit)},
	}
	var resp struct {
		Result Facets `json:"result"`
	}
	if err := c.getJSON(ctx, "package_search", params, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

// ResourceShow trae los metadatos de un recurso.
func (c *Client) ResourceShow(ctx context.Context, id string) (*Resource, error) {
	var r struct {
		Result Resource `json:"result"`
	}
	body, err := c.Get(ctx, "resource_show", url.Values{"id": {id}})
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(body, &r) != nil {
		return nil, ErrDecode
	}
	return &r.Result, nil
}

// ErrDecode indica que CKAN respondió 200 pero con un JSON que no se pudo leer.
var ErrDecode = errors.New("respuesta ilegible de datos.gob.cl")

// DSField es un campo de un recurso DataStore.
type DSField struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// DSResult es la respuesta de datastore_search.
type DSResult struct {
	Total   int              `json:"total"`
	Fields  []DSField        `json:"fields"`
	Records []map[string]any `json:"records"`
}

// Columns son los campos visibles (sin los internos de CKAN).
func (r *DSResult) Columns() []string {
	var cols []string
	for _, f := range r.Fields {
		if !InternalField(f.ID) {
			cols = append(cols, f.ID)
		}
	}
	return cols
}

// InternalField dice si el campo es interno de DataStore (_id, _full_text, rank).
func InternalField(id string) bool { return id == "_id" || id == "_full_text" || id == "rank" }

// DatastoreSearch consulta un recurso con DataStore.
func (c *Client) DatastoreSearch(ctx context.Context, rid string, params url.Values) (*DSResult, error) {
	params.Set("resource_id", rid)
	var resp struct {
		Result DSResult `json:"result"`
	}
	if err := c.getJSON(ctx, "datastore_search", params, &resp); err != nil {
		return nil, err
	}
	return &resp.Result, nil
}

// StringifyFilters convierte los valores de un JSON de filtros a string (DataStore compara texto exacto;
// un número contra un campo text da "Invalid query"). Acepta listas de valores.
func StringifyFilters(raw string) (string, error) {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return "", fmt.Errorf(`"filtros" debe ser un objeto JSON, ej: {"Region":"13","Tipo":"SpA"} (%v)`, err)
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		switch t := v.(type) {
		case []any:
			l := make([]string, 0, len(t))
			for _, e := range t {
				l = append(l, scalarString(e))
			}
			out[k] = l
		default:
			out[k] = scalarString(t)
		}
	}
	b, err := json.Marshal(out)
	return string(b), err
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}
