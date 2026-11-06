package main

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

	"github.com/mark3labs/mcp-go/mcp"
)

// ===================== Mercado Público / ChileCompra =====================

const mpBase = "https://api.mercadopublico.cl/servicios/v1/publico"

// MP rechaza peticiones simultáneas con el mismo ticket (Codigo 10500, HTTP 429): se serializan y se reintenta más.
var (
	fuenteMP = fuente{"Mercado Público", 20 * time.Second, 4, 1500 * time.Millisecond}
	mpMu     sync.Mutex
)

// Estados aceptados por la API (verificados contra la API en vivo, 2026-11).
var (
	estadosLicitacion = []string{"activas", "publicada", "cerrada", "desierta", "adjudicada", "revocada", "suspendida", "todos"}
	estadosOC         = []string{"enviadaproveedor", "aceptada", "cancelada", "recepcionconforme", "pendienterecepcion", "todos"}
)

var codigoEstadoLicitacion = map[int]string{5: "Publicada", 6: "Cerrada", 7: "Desierta", 8: "Adjudicada", 18: "Revocada", 19: "Suspendida"}
var codigoEstadoOC = map[int]string{4: "Enviada a proveedor", 5: "En proceso", 6: "Aceptada", 9: "Cancelada", 12: "Recepción conforme",
	13: "Pendiente de recepcionar", 14: "Recepcionada parcialmente", 15: "Recepción conforme incompleta"}

const sinTicket = "MERCADO_PUBLICO_TICKET no configurado. Pedí tu ticket gratis en https://api.mercadopublico.cl (con Clave Única) y agregalo al 'env' del servidor MCP."

func mpTicket() string {
	return strings.TrimSpace(os.Getenv("MERCADO_PUBLICO_TICKET"))
}

// mpAPIError es un error de negocio de MP ({"Codigo":..., "Mensaje":...}).
type mpAPIError struct {
	codigo  int
	mensaje string
}

func (e *mpAPIError) Error() string { return mpErrorMessage(e.codigo, e.mensaje) }

// mpErrorMessage traduce Codigo/Mensaje de Mercado Público a una frase con qué hacer.
func mpErrorMessage(codigo int, mensaje string) string {
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

func parseMPError(body []byte) *mpAPIError {
	var e struct {
		Codigo  *int   `json:"Codigo"`
		Mensaje string `json:"Mensaje"`
	}
	if json.Unmarshal(body, &e) != nil || e.Codigo == nil {
		return nil
	}
	return &mpAPIError{*e.Codigo, e.Mensaje}
}

// mpGet llama a MP con el ticket, serializado, y traduce errores. El ticket nunca aparece en los mensajes.
func mpGet(ctx context.Context, path string, params url.Values) ([]byte, error) {
	ticket := mpTicket()
	if ticket == "" {
		return nil, errors.New(sinTicket)
	}
	params.Set("ticket", ticket)
	mpMu.Lock()
	defer mpMu.Unlock()
	body, err := doGet(ctx, fuenteMP, mpBase+path+"?"+params.Encode())
	var he *httpError
	if errors.As(err, &he) {
		if me := parseMPError(he.body); me != nil {
			return nil, me
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if me := parseMPError(body); me != nil && me.codigo != 0 {
		return nil, me
	}
	return body, nil
}

func noResults(err error) bool {
	var me *mpAPIError
	return errors.As(err, &me) && me.codigo == 10200
}

var fechaMPRe = regexp.MustCompile(`^\d{8}$`)

// validarFechaMP acepta ddmmaaaa (también dd-mm-aaaa, dd/mm/aaaa o aaaa-mm-dd) y devuelve ddmmaaaa.
func validarFechaMP(s string) (string, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil { // ISO aaaa-mm-dd, común en LLMs
		return t.Format("02012006"), nil
	}
	s = strings.NewReplacer("-", "", "/", "", ".", "").Replace(s)
	if !fechaMPRe.MatchString(s) {
		return "", fmt.Errorf("fecha debe ir como ddmmaaaa (8 dígitos), ej: '05112026' para el 5 de noviembre de 2026")
	}
	if _, err := time.Parse("02012006", s); err != nil {
		return "", fmt.Errorf("fecha %q no es un día válido (formato ddmmaaaa, ej: '05112026')", s)
	}
	return s, nil
}

// mpPagina recorta un listado según limite/pagina y arma el encabezado "mostrando X-Y de N".
func mpPagina(total, limite, pagina int) (from, to int, nota string) {
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

type mpComprador struct {
	CodigoOrganismo string `json:"CodigoOrganismo"`
	NombreOrganismo string `json:"NombreOrganismo"`
	NombreUnidad    string `json:"NombreUnidad"`
	ComunaUnidad    string `json:"ComunaUnidad"`
	RegionUnidad    string `json:"RegionUnidad"`
}

type mpLicitacion struct {
	CodigoExterno string       `json:"CodigoExterno"`
	Nombre        string       `json:"Nombre"`
	CodigoEstado  int          `json:"CodigoEstado"`
	Estado        string       `json:"Estado"`
	Descripcion   string       `json:"Descripcion"`
	FechaCierre   string       `json:"FechaCierre"`
	Tipo          string       `json:"Tipo"`
	Moneda        string       `json:"Moneda"`
	MontoEstimado float64      `json:"MontoEstimado"`
	Comprador     *mpComprador `json:"Comprador"`
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

func fechaMP(s string) string {
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
}

func estadoLic(l mpLicitacion) string {
	if l.Estado != "" {
		return l.Estado
	}
	if s, ok := codigoEstadoLicitacion[l.CodigoEstado]; ok {
		return s
	}
	return strconv.Itoa(l.CodigoEstado)
}

func pesos(v float64, moneda string) string {
	s := strconv.FormatFloat(v, 'f', 0, 64)
	var out []byte
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	return "$" + string(out) + " " + moneda
}

func buscarLicitaciones(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if mpTicket() == "" {
		return errMsg(sinTicket), nil
	}
	params := url.Values{}
	if f := stringArg(req, "fecha"); f != "" {
		fecha, err := validarFechaMP(f)
		if err != nil {
			return errResult(err), nil
		}
		params.Set("fecha", fecha)
	}
	if e := strings.ToLower(stringArg(req, "estado")); e != "" {
		if !contains(estadosLicitacion, e) {
			return errMsg("estado %q no válido. Usá uno de: %s", e, strings.Join(estadosLicitacion, ", ")), nil
		}
		params.Set("estado", e)
	}
	codigo := stringArg(req, "codigo")
	if codigo != "" {
		params.Set("codigo", codigo)
	}
	if v := stringArg(req, "codigo_organismo"); v != "" {
		params.Set("CodigoOrganismo", v)
	}
	if v := stringArg(req, "codigo_proveedor"); v != "" {
		params.Set("CodigoProveedor", v)
	}
	limite := limitArg(req, "limite", 25, 100)
	pagina := offsetArg(req, "pagina", 1, 1, 100_000)

	body, err := mpGet(ctx, "/licitaciones.json", params)
	if noResults(err) {
		return textResult("No se encontraron licitaciones con esos filtros."), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	var resp struct {
		Cantidad int            `json:"Cantidad"`
		Listado  []mpLicitacion `json:"Listado"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de Mercado Público: %v", err), nil
	}
	total := len(resp.Listado)
	if total == 0 {
		return textResult("No se encontraron licitaciones con esos filtros."), nil
	}

	var sb strings.Builder
	if total == 1 && resp.Listado[0].Comprador != nil {
		l := resp.Listado[0]
		fmt.Fprintf(&sb, "## %s\n", cleanText(l.Nombre))
		fmt.Fprintf(&sb, "- Código: **%s** | Tipo: %s | Estado: %s\n", l.CodigoExterno, l.Tipo, estadoLic(l))
		c := l.Comprador
		fmt.Fprintf(&sb, "- Organismo: %s (código %s)\n- Unidad: %s, %s, %s\n", cleanText(c.NombreOrganismo), c.CodigoOrganismo, cleanText(c.NombreUnidad), cleanText(c.ComunaUnidad), cleanText(c.RegionUnidad))
		if l.MontoEstimado > 0 {
			fmt.Fprintf(&sb, "- Monto estimado: %s\n", pesos(l.MontoEstimado, l.Moneda))
		}
		if f := l.Fechas; f != nil {
			fmt.Fprintf(&sb, "- Publicación: %s | Cierre: %s | Adjudicación (estimada): %s\n", fechaMP(f.FechaPublicacion), fechaMP(f.FechaCierre), fechaMP(orDefault(f.FechaAdjudicacion, f.FechaEstimadaAdjudicacion)))
		}
		if a := l.Adjudicacion; a != nil {
			fmt.Fprintf(&sb, "- Adjudicada: %s, %d oferentes, acta: %s\n", fechaMP(a.Fecha), a.NumeroOferentes, a.UrlActa)
		}
		fmt.Fprintf(&sb, "- Descripción: %s\n", truncate(cleanText(l.Descripcion), 1500))
		return textResult(sb.String()), nil
	}

	from, to, nota := mpPagina(total, limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d licitaciones (%d páginas de %d).", pagina, total, (total+limite-1)/limite, limite), nil
	}
	fmt.Fprintf(&sb, "## Licitaciones — %d resultados\n%s\nPara organismo, monto y fechas de una licitación, consultá con codigo=<Código>.\n\n", total, nota)
	sb.WriteString(mdHeader("Código", "Nombre", "Estado", "Cierre"))
	for _, l := range resp.Listado[from:to] {
		sb.WriteString(mdRow(l.CodigoExterno, cell(l.Nombre, 90), estadoLic(l), fechaMP(l.FechaCierre)))
	}
	return textResult(sb.String()), nil
}

type mpOrden struct {
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
	Comprador *mpComprador `json:"Comprador"`
	Proveedor *struct {
		Codigo      string `json:"Codigo"`
		Nombre      string `json:"Nombre"`
		RutSucursal string `json:"RutSucursal"`
		Actividad   string `json:"Actividad"`
	} `json:"Proveedor"`
}

func estadoOC(o mpOrden) string {
	if o.Estado != "" {
		return o.Estado
	}
	if s, ok := codigoEstadoOC[o.CodigoEstado]; ok {
		return s
	}
	return strconv.Itoa(o.CodigoEstado)
}

func buscarOrdenesCompra(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if mpTicket() == "" {
		return errMsg(sinTicket), nil
	}
	params := url.Values{}
	if f := stringArg(req, "fecha"); f != "" {
		fecha, err := validarFechaMP(f)
		if err != nil {
			return errResult(err), nil
		}
		params.Set("fecha", fecha)
	}
	if e := strings.ToLower(stringArg(req, "estado")); e != "" {
		if !contains(estadosOC, e) {
			return errMsg("estado %q no válido. Usá uno de: %s", e, strings.Join(estadosOC, ", ")), nil
		}
		params.Set("estado", e)
	}
	if v := stringArg(req, "codigo"); v != "" {
		params.Set("codigo", v)
	}
	if v := stringArg(req, "codigo_organismo"); v != "" {
		params.Set("CodigoOrganismo", v)
	}
	if v := stringArg(req, "codigo_proveedor"); v != "" {
		params.Set("CodigoProveedor", v)
	}
	limite := limitArg(req, "limite", 25, 100)
	pagina := offsetArg(req, "pagina", 1, 1, 100_000)

	body, err := mpGet(ctx, "/ordenesdecompra.json", params)
	if noResults(err) {
		return textResult("No se encontraron órdenes de compra con esos filtros."), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	var resp struct {
		Cantidad int       `json:"Cantidad"`
		Listado  []mpOrden `json:"Listado"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de Mercado Público: %v", err), nil
	}
	total := len(resp.Listado)
	if total == 0 {
		return textResult("No se encontraron órdenes de compra con esos filtros."), nil
	}

	var sb strings.Builder
	if total == 1 && resp.Listado[0].Comprador != nil {
		o := resp.Listado[0]
		fmt.Fprintf(&sb, "## %s\n", cleanText(o.Nombre))
		fmt.Fprintf(&sb, "- Código: **%s** | Tipo: %s | Estado: %s\n", o.Codigo, o.Tipo, estadoOC(o))
		c := o.Comprador
		fmt.Fprintf(&sb, "- Organismo: %s (código %s) — %s, %s\n", cleanText(c.NombreOrganismo), c.CodigoOrganismo, cleanText(c.NombreUnidad), cleanText(c.RegionUnidad))
		if p := o.Proveedor; p != nil {
			fmt.Fprintf(&sb, "- Proveedor: %s (código %s, RUT %s) — %s\n", cleanText(p.Nombre), p.Codigo, p.RutSucursal, cleanText(p.Actividad))
		}
		fmt.Fprintf(&sb, "- Total neto: %s | Total: %s\n", pesos(o.TotalNeto, o.TipoMoneda), pesos(o.Total, o.TipoMoneda))
		if f := o.Fechas; f != nil {
			fmt.Fprintf(&sb, "- Creación: %s | Envío: %s | Aceptación: %s\n", fechaMP(f.FechaCreacion), fechaMP(f.FechaEnvio), fechaMP(f.FechaAceptacion))
		}
		if o.CodigoLicitacion != "" {
			fmt.Fprintf(&sb, "- Licitación asociada: %s\n", o.CodigoLicitacion)
		}
		fmt.Fprintf(&sb, "- Descripción: %s\n", truncate(cleanText(o.Descripcion), 1000))
		return textResult(sb.String()), nil
	}

	from, to, nota := mpPagina(total, limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d órdenes (%d páginas de %d).", pagina, total, (total+limite-1)/limite, limite), nil
	}
	fmt.Fprintf(&sb, "## Órdenes de compra — %d resultados\n%s\nPara organismo, proveedor y montos de una orden, consultá con codigo=<Código>.\n\n", total, nota)
	sb.WriteString(mdHeader("Código", "Nombre", "Estado"))
	for _, o := range resp.Listado[from:to] {
		sb.WriteString(mdRow(o.Codigo, cell(o.Nombre, 100), estadoOC(o)))
	}
	return textResult(sb.String()), nil
}

type mpEmpresa struct {
	CodigoEmpresa string `json:"CodigoEmpresa"`
	NombreEmpresa string `json:"NombreEmpresa"`
}

func buscarProveedor(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if mpTicket() == "" {
		return errMsg(sinTicket), nil
	}
	rut, err := normRUT(stringArg(req, "rut"))
	if err != nil {
		return errResult(err), nil
	}
	mpRut := rutConPuntos(rut) // la API solo encuentra el RUT con puntos
	body, err := mpGet(ctx, "/Empresas/BuscarProveedor", url.Values{"rutempresaproveedor": {mpRut}})
	if noResults(err) {
		return textResult(fmt.Sprintf("No hay un proveedor del Estado registrado con RUT %s en Mercado Público.", rut)), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	var resp struct {
		ListaEmpresas []mpEmpresa `json:"listaEmpresas"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de Mercado Público: %v", err), nil
	}
	if len(resp.ListaEmpresas) == 0 {
		return textResult(fmt.Sprintf("No hay un proveedor del Estado registrado con RUT %s en Mercado Público.", rut)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Proveedor del Estado — RUT %s\n\n", rut)
	sb.WriteString(mdHeader("Código proveedor", "Nombre"))
	for _, e := range resp.ListaEmpresas {
		sb.WriteString(mdRow(e.CodigoEmpresa, cell(e.NombreEmpresa, 100)))
	}
	sb.WriteString("\nUsá el código en licitaciones(codigo_proveedor=...) u ordenes_compra(codigo_proveedor=..., fecha=...).\n")
	return textResult(sb.String()), nil
}

var (
	orgMu     sync.Mutex
	orgCache  []mpEmpresa
	orgCacheT time.Time
)

func listarOrganismos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if mpTicket() == "" {
		return errMsg(sinTicket), nil
	}
	q := normText(stringArg(req, "q"))
	limite := limitArg(req, "limite", 50, 500)
	pagina := offsetArg(req, "pagina", 1, 1, 10_000)

	orgMu.Lock()
	orgs := orgCache
	if orgs == nil || time.Since(orgCacheT) > 6*time.Hour {
		body, err := mpGet(ctx, "/Empresas/BuscarComprador", url.Values{})
		if err != nil {
			orgMu.Unlock()
			return errResult(err), nil
		}
		var resp struct {
			ListaEmpresas []mpEmpresa `json:"listaEmpresas"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			orgMu.Unlock()
			return errMsg("respuesta inesperada de Mercado Público: %v", err), nil
		}
		orgs, orgCache, orgCacheT = resp.ListaEmpresas, resp.ListaEmpresas, time.Now()
	}
	orgMu.Unlock()

	var match []mpEmpresa
	for _, o := range orgs {
		if q == "" || strings.Contains(normText(o.NombreEmpresa), q) || o.CodigoEmpresa == q {
			match = append(match, o)
		}
	}
	if len(match) == 0 {
		return textResult(fmt.Sprintf("Ningún organismo de los %d coincide con %q. Probá con una palabra (ej: 'salud', 'municipalidad de temuco').", len(orgs), stringArg(req, "q"))), nil
	}
	from, to, nota := mpPagina(len(match), limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d organismos.", pagina, len(match)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Organismos públicos en Mercado Público — %d coinciden (de %d)\n%s\n\n", len(match), len(orgs), nota)
	sb.WriteString(mdHeader("Código", "Nombre"))
	for _, o := range match[from:to] {
		sb.WriteString(mdRow(o.CodigoEmpresa, cell(o.NombreEmpresa, 100)))
	}
	sb.WriteString("\nUsá el código en licitaciones(codigo_organismo=...) u ordenes_compra(codigo_organismo=...).\n")
	return textResult(sb.String()), nil
}
