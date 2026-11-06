// Package mercadopublico es el cliente de la API de Mercado Público / ChileCompra. Requiere un ticket
// (MERCADO_PUBLICO_TICKET) que nunca aparece en los mensajes de error.
package mercadopublico

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// BaseURL es la API pública de Mercado Público.
const BaseURL = "https://api.mercadopublico.cl/servicios/v1/publico"

// FuenteMP: MP rechaza peticiones simultáneas con el mismo ticket (Codigo 10500, HTTP 429): se serializan y se reintenta más.
var FuenteMP = httpx.Fuente{Nombre: "Mercado Público", Timeout: 20 * time.Second, Intentos: 4, Espera: 1500 * time.Millisecond}

// Estados aceptados por la API (verificados contra la API en vivo, 2026-11).
var (
	EstadosLicitacion = []string{"activas", "publicada", "cerrada", "desierta", "adjudicada", "revocada", "suspendida", "todos"}
	EstadosOC         = []string{"enviadaproveedor", "aceptada", "cancelada", "recepcionconforme", "pendienterecepcion", "todos"}
)

var codigoEstadoLicitacion = map[int]string{5: "Publicada", 6: "Cerrada", 7: "Desierta", 8: "Adjudicada", 18: "Revocada", 19: "Suspendida"}
var codigoEstadoOC = map[int]string{4: "Enviada a proveedor", 5: "En proceso", 6: "Aceptada", 9: "Cancelada", 12: "Recepción conforme",
	13: "Pendiente de recepcionar", 14: "Recepcionada parcialmente", 15: "Recepción conforme incompleta"}

// SinTicket es el mensaje cuando no hay MERCADO_PUBLICO_TICKET.
const SinTicket = "MERCADO_PUBLICO_TICKET no configurado. Pedí tu ticket gratis en https://api.mercadopublico.cl (con Clave Única) y agregalo al 'env' del servidor MCP."

// TicketFromEnv lee MERCADO_PUBLICO_TICKET.
func TicketFromEnv() string {
	return strings.TrimSpace(os.Getenv("MERCADO_PUBLICO_TICKET"))
}

// Client llama a MP con el ticket, de a una petición por vez. Cachea el listado de organismos.
type Client struct {
	HTTP    *httpx.Client
	BaseURL string
	Fuente  httpx.Fuente
	Ticket  func() string

	mu sync.Mutex

	orgMu     sync.Mutex
	orgCache  []Empresa
	orgCacheT time.Time
}

// New crea un cliente que lee el ticket del entorno en cada llamada.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, BaseURL: BaseURL, Fuente: FuenteMP, Ticket: TicketFromEnv}
}

// TieneTicket dice si hay ticket configurado.
func (c *Client) TieneTicket() bool { return c.Ticket() != "" }

// APIError es un error de negocio de MP ({"Codigo":..., "Mensaje":...}).
type APIError struct {
	Codigo  int
	Mensaje string
}

func (e *APIError) Error() string { return ErrorMessage(e.Codigo, e.Mensaje) }

// ErrorMessage traduce Codigo/Mensaje de Mercado Público a una frase con qué hacer.
func ErrorMessage(codigo int, mensaje string) string {
	switch codigo {
	case 203:
		return "El MERCADO_PUBLICO_TICKET no es válido (Mercado Público: \"" + mensaje + "\"). Revisalo o pedí uno nuevo en https://api.mercadopublico.cl con Clave Única."
	case 10500:
		return "Mercado Público está rechazando consultas simultáneas con este ticket (límite de uso). Esperá unos segundos y reintentá; si el ticket es compartido, pedí uno propio en https://api.mercadopublico.cl."
	case 10200:
		return "Mercado Público no encontró resultados."
	case 10300, 10000:
		return "Mercado Público rechazó los parámetros (\"" + mensaje + "\"). Revisá: fecha ddmmaaaa, estado de la lista permitida y códigos existentes."
	}
	if mensaje != "" {
		return fmt.Sprintf("Mercado Público respondió: %s (código %d)", mensaje, codigo)
	}
	return fmt.Sprintf("Mercado Público respondió con error (código %d)", codigo)
}

// ParseError lee {"Codigo":..., "Mensaje":...}. Devuelve nil si el cuerpo no es un error de MP.
func ParseError(body []byte) *APIError {
	var e struct {
		Codigo  *int   `json:"Codigo"`
		Mensaje string `json:"Mensaje"`
	}
	if json.Unmarshal(body, &e) != nil || e.Codigo == nil {
		return nil
	}
	return &APIError{*e.Codigo, e.Mensaje}
}

// Get llama a MP con el ticket, serializado, y traduce errores. El ticket nunca aparece en los mensajes.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	ticket := c.Ticket()
	if ticket == "" {
		//lint:ignore ST1005 es un mensaje para el LLM, igual al que devuelven las tools
		return nil, errors.New(SinTicket)
	}
	params.Set("ticket", ticket)
	c.mu.Lock()
	defer c.mu.Unlock()
	body, err := c.HTTP.Get(ctx, c.Fuente, c.BaseURL+path+"?"+params.Encode())
	var he *httpx.HTTPError
	if errors.As(err, &he) {
		if me := ParseError(he.Body); me != nil {
			return nil, me
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if me := ParseError(body); me != nil && me.Codigo != 0 {
		return nil, me
	}
	return body, nil
}

// NoResults dice si err es el "sin resultados" de MP (Codigo 10200).
func NoResults(err error) bool {
	var me *APIError
	return errors.As(err, &me) && me.Codigo == 10200
}

var fechaRe = regexp.MustCompile(`^\d{8}$`)

// ValidarFecha acepta ddmmaaaa (también dd-mm-aaaa, dd/mm/aaaa o aaaa-mm-dd) y devuelve ddmmaaaa.
func ValidarFecha(s string) (string, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil { // ISO aaaa-mm-dd, común en LLMs
		return t.Format("02012006"), nil
	}
	s = strings.NewReplacer("-", "", "/", "", ".", "").Replace(s)
	if !fechaRe.MatchString(s) {
		return "", fmt.Errorf("fecha debe ir como ddmmaaaa (8 dígitos), ej: '05112026' para el 5 de noviembre de 2026")
	}
	if _, err := time.Parse("02012006", s); err != nil {
		return "", fmt.Errorf("fecha %q no es un día válido (formato ddmmaaaa, ej: '05112026')", s)
	}
	return s, nil
}

// Pagina recorta un listado según limite/pagina y arma el encabezado "mostrando X-Y de N".
func Pagina(total, limite, pagina int) (from, to int, nota string) {
	from = (pagina - 1) * limite
	if from >= total {
		return 0, 0, ""
	}
	to = min(total, from+limite)
	nota = fmt.Sprintf("Mostrando %d-%d de %d.", from+1, to, total)
	if to < total {
		nota += fmt.Sprintf(" Siguiente: pagina=%d.", pagina+1)
	}
	return from, to, nota
}

// Comprador es el organismo y unidad que compra.
type Comprador struct {
	CodigoOrganismo string `json:"CodigoOrganismo"`
	NombreOrganismo string `json:"NombreOrganismo"`
	NombreUnidad    string `json:"NombreUnidad"`
	ComunaUnidad    string `json:"ComunaUnidad"`
	RegionUnidad    string `json:"RegionUnidad"`
}

// Licitacion es una licitación (listado o detalle; el detalle trae Comprador).
type Licitacion struct {
	CodigoExterno string     `json:"CodigoExterno"`
	Nombre        string     `json:"Nombre"`
	CodigoEstado  int        `json:"CodigoEstado"`
	Estado        string     `json:"Estado"`
	Descripcion   string     `json:"Descripcion"`
	FechaCierre   string     `json:"FechaCierre"`
	Tipo          string     `json:"Tipo"`
	Moneda        string     `json:"Moneda"`
	MontoEstimado float64    `json:"MontoEstimado"`
	Comprador     *Comprador `json:"Comprador"`
	Fechas        *struct {
		FechaPublicacion          string `json:"FechaPublicacion"`
		FechaCierre               string `json:"FechaCierre"`
		FechaAdjudicacion         string `json:"FechaAdjudicacion"`
		FechaEstimadaAdjudicacion string `json:"FechaEstimadaAdjudicacion"`
	} `json:"Fechas"`
	Adjudicacion *struct {
		Fecha           string `json:"Fecha"`
		Numero          string `json:"Numero"`
		NumeroOferentes int    `json:"NumeroOferentes"`
		UrlActa         string `json:"UrlActa"`
	} `json:"Adjudicacion"`
}

// EstadoTexto es el estado legible (la API a veces solo trae CodigoEstado).
func (l Licitacion) EstadoTexto() string {
	if l.Estado != "" {
		return l.Estado
	}
	if s, ok := codigoEstadoLicitacion[l.CodigoEstado]; ok {
		return s
	}
	return strconv.Itoa(l.CodigoEstado)
}

// Orden es una orden de compra (listado o detalle; el detalle trae Comprador).
type Orden struct {
	Codigo           string  `json:"Codigo"`
	Nombre           string  `json:"Nombre"`
	CodigoEstado     int     `json:"CodigoEstado"`
	Estado           string  `json:"Estado"`
	CodigoLicitacion string  `json:"CodigoLicitacion"`
	Descripcion      string  `json:"Descripcion"`
	Tipo             string  `json:"Tipo"`
	TipoMoneda       string  `json:"TipoMoneda"`
	TotalNeto        float64 `json:"TotalNeto"`
	Total            float64 `json:"Total"`
	Fechas           *struct {
		FechaCreacion   string `json:"FechaCreacion"`
		FechaEnvio      string `json:"FechaEnvio"`
		FechaAceptacion string `json:"FechaAceptacion"`
	} `json:"Fechas"`
	Comprador *Comprador `json:"Comprador"`
	Proveedor *struct {
		Codigo      string `json:"Codigo"`
		Nombre      string `json:"Nombre"`
		RutSucursal string `json:"RutSucursal"`
		Actividad   string `json:"Actividad"`
	} `json:"Proveedor"`
}

// EstadoTexto es el estado legible (la API a veces solo trae CodigoEstado).
func (o Orden) EstadoTexto() string {
	if o.Estado != "" {
		return o.Estado
	}
	if s, ok := codigoEstadoOC[o.CodigoEstado]; ok {
		return s
	}
	return strconv.Itoa(o.CodigoEstado)
}

// Empresa es un proveedor u organismo comprador.
type Empresa struct {
	CodigoEmpresa string `json:"CodigoEmpresa"`
	NombreEmpresa string `json:"NombreEmpresa"`
}

func (c *Client) getJSON(ctx context.Context, path string, params url.Values, v any) error {
	body, err := c.Get(ctx, path, params)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("respuesta inesperada de Mercado Público: %v", err)
	}
	return nil
}

// Licitaciones busca licitaciones. params usa los nombres de la API (fecha, estado, codigo, CodigoOrganismo, CodigoProveedor).
func (c *Client) Licitaciones(ctx context.Context, params url.Values) ([]Licitacion, error) {
	var resp struct {
		Cantidad int          `json:"Cantidad"`
		Listado  []Licitacion `json:"Listado"`
	}
	if err := c.getJSON(ctx, "/licitaciones.json", params, &resp); err != nil {
		return nil, err
	}
	return resp.Listado, nil
}

// Ordenes busca órdenes de compra. params usa los nombres de la API.
func (c *Client) Ordenes(ctx context.Context, params url.Values) ([]Orden, error) {
	var resp struct {
		Cantidad int     `json:"Cantidad"`
		Listado  []Orden `json:"Listado"`
	}
	if err := c.getJSON(ctx, "/ordenesdecompra.json", params, &resp); err != nil {
		return nil, err
	}
	return resp.Listado, nil
}

// BuscarProveedor busca un proveedor por RUT con puntos ("70.017.820-k"; la API solo lo encuentra así).
func (c *Client) BuscarProveedor(ctx context.Context, rutConPuntos string) ([]Empresa, error) {
	var resp struct {
		ListaEmpresas []Empresa `json:"listaEmpresas"`
	}
	if err := c.getJSON(ctx, "/Empresas/BuscarProveedor", url.Values{"rutempresaproveedor": {rutConPuntos}}, &resp); err != nil {
		return nil, err
	}
	return resp.ListaEmpresas, nil
}

// Organismos lista los organismos compradores (cache de 6 h).
func (c *Client) Organismos(ctx context.Context) ([]Empresa, error) {
	c.orgMu.Lock()
	defer c.orgMu.Unlock()
	if c.orgCache != nil && time.Since(c.orgCacheT) <= 6*time.Hour {
		return c.orgCache, nil
	}
	var resp struct {
		ListaEmpresas []Empresa `json:"listaEmpresas"`
	}
	if err := c.getJSON(ctx, "/Empresas/BuscarComprador", url.Values{}, &resp); err != nil {
		return nil, err
	}
	c.orgCache, c.orgCacheT = resp.ListaEmpresas, time.Now()
	return resp.ListaEmpresas, nil
}
