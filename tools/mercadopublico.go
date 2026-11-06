package tools

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	mp "github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mercadopublico"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== Mercado Público / ChileCompra =====================

func fechaMP(s string) string {
	s = strings.Replace(s, "T", " ", 1)
	if len(s) > 16 {
		s = s[:16]
	}
	return s
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

// mpParams arma los filtros comunes de licitaciones y órdenes con los nombres de la API.
func mpParams(req mcp.CallToolRequest, estados []string) (url.Values, *mcp.CallToolResult) {
	params := url.Values{}
	if f := stringArg(req, "fecha"); f != "" {
		fecha, err := mp.ValidarFecha(f)
		if err != nil {
			return nil, errResult(err)
		}
		params.Set("fecha", fecha)
	}
	if e := strings.ToLower(stringArg(req, "estado")); e != "" {
		if !slices.Contains(estados, e) {
			return nil, errMsg("estado %q no válido. Usá uno de: %s", e, strings.Join(estados, ", "))
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
	return params, nil
}

// MPItem es una fila de un listado de Mercado Público.
type MPItem struct {
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
	Estado string `json:"estado"`
	Cierre string `json:"cierre,omitempty" jsonschema:"description=Fecha de cierre aaaa-mm-dd hh:mm (solo licitaciones)"`
}

// MPOrganismo es el comprador de una licitación u orden.
type MPOrganismo struct {
	Codigo string `json:"codigo"`
	Nombre string `json:"nombre"`
	Unidad string `json:"unidad,omitempty"`
	Comuna string `json:"comuna,omitempty"`
	Region string `json:"region,omitempty"`
}

// LicitacionDetalle es el detalle de una licitación (cuando se consulta por código).
type LicitacionDetalle struct {
	MPItem
	Tipo              string      `json:"tipo"`
	Organismo         MPOrganismo `json:"organismo"`
	MontoEstimado     float64     `json:"monto_estimado,omitempty"`
	Moneda            string      `json:"moneda,omitempty"`
	FechaPublicacion  string      `json:"fecha_publicacion,omitempty"`
	FechaAdjudicacion string      `json:"fecha_adjudicacion,omitempty" jsonschema:"description=Real o estimada"`
	Oferentes         int         `json:"oferentes,omitempty"`
	URLActa           string      `json:"url_acta,omitempty"`
	Descripcion       string      `json:"descripcion"`
}

// LicitacionesOut es la salida estructurada de licitaciones.
type LicitacionesOut struct {
	Total        int                `json:"total"`
	Pagina       int                `json:"pagina"`
	Limite       int                `json:"limite"`
	Licitaciones []MPItem           `json:"licitaciones" jsonschema:"description=Filas de esta página"`
	Detalle      *LicitacionDetalle `json:"detalle,omitempty" jsonschema:"description=Presente cuando la búsqueda devuelve una sola licitación con detalle (por codigo)"`
}

func (h *handlers) buscarLicitaciones(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !h.MP.TieneTicket() {
		return errMsg(mp.SinTicket), nil
	}
	params, res := mpParams(req, mp.EstadosLicitacion)
	if res != nil {
		return res, nil
	}
	limite := limitArg(req, "limite", 25, 100)
	pagina := offsetArg(req, "pagina", 1, 1, 100_000)

	st := LicitacionesOut{Pagina: pagina, Limite: limite, Licitaciones: []MPItem{}}
	listado, err := h.MP.Licitaciones(ctx, params)
	if mp.NoResults(err) {
		return structResult(st, "No se encontraron licitaciones con esos filtros."), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	total := len(listado)
	st.Total = total
	if total == 0 {
		return structResult(st, "No se encontraron licitaciones con esos filtros."), nil
	}

	item := func(l mp.Licitacion) MPItem {
		return MPItem{Codigo: l.CodigoExterno, Nombre: text.CleanText(l.Nombre), Estado: l.EstadoTexto(), Cierre: fechaMP(l.FechaCierre)}
	}
	var sb strings.Builder
	if total == 1 && listado[0].Comprador != nil {
		l := listado[0]
		c := l.Comprador
		d := &LicitacionDetalle{MPItem: item(l), Tipo: l.Tipo, Descripcion: text.CleanText(l.Descripcion),
			Organismo: MPOrganismo{c.CodigoOrganismo, text.CleanText(c.NombreOrganismo), text.CleanText(c.NombreUnidad), text.CleanText(c.ComunaUnidad), text.CleanText(c.RegionUnidad)}}
		fmt.Fprintf(&sb, "## %s\n", text.CleanText(l.Nombre))
		fmt.Fprintf(&sb, "- Código: **%s** | Tipo: %s | Estado: %s\n", l.CodigoExterno, l.Tipo, l.EstadoTexto())
		fmt.Fprintf(&sb, "- Organismo: %s (código %s)\n- Unidad: %s, %s, %s\n", text.CleanText(c.NombreOrganismo), c.CodigoOrganismo, text.CleanText(c.NombreUnidad), text.CleanText(c.ComunaUnidad), text.CleanText(c.RegionUnidad))
		if l.MontoEstimado > 0 {
			d.MontoEstimado, d.Moneda = l.MontoEstimado, l.Moneda
			fmt.Fprintf(&sb, "- Monto estimado: %s\n", pesos(l.MontoEstimado, l.Moneda))
		}
		if f := l.Fechas; f != nil {
			d.FechaPublicacion, d.FechaAdjudicacion = fechaMP(f.FechaPublicacion), fechaMP(text.OrDefault(f.FechaAdjudicacion, f.FechaEstimadaAdjudicacion))
			if d.Cierre == "" {
				d.Cierre = fechaMP(f.FechaCierre)
			}
			fmt.Fprintf(&sb, "- Publicación: %s | Cierre: %s | Adjudicación (estimada): %s\n", fechaMP(f.FechaPublicacion), fechaMP(f.FechaCierre), fechaMP(text.OrDefault(f.FechaAdjudicacion, f.FechaEstimadaAdjudicacion)))
		}
		if a := l.Adjudicacion; a != nil {
			d.Oferentes, d.URLActa = a.NumeroOferentes, a.UrlActa
			fmt.Fprintf(&sb, "- Adjudicada: %s, %d oferentes, acta: %s\n", fechaMP(a.Fecha), a.NumeroOferentes, a.UrlActa)
		}
		fmt.Fprintf(&sb, "- Descripción: %s\n", text.Truncate(text.CleanText(l.Descripcion), 1500))
		st.Licitaciones = append(st.Licitaciones, item(l))
		st.Pagina, st.Detalle = 1, d
		return structResult(st, sb.String()), nil
	}

	from, to, nota := mp.Pagina(total, limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d licitaciones (%d páginas de %d).", pagina, total, (total+limite-1)/limite, limite), nil
	}
	fmt.Fprintf(&sb, "## Licitaciones — %d resultados\n%s\nPara organismo, monto y fechas de una licitación, consultá con codigo=<Código>.\n\n", total, nota)
	sb.WriteString(text.MDHeader("Código", "Nombre", "Estado", "Cierre"))
	for _, l := range listado[from:to] {
		sb.WriteString(text.MDRow(l.CodigoExterno, text.Cell(l.Nombre, 90), l.EstadoTexto(), fechaMP(l.FechaCierre)))
		st.Licitaciones = append(st.Licitaciones, item(l))
	}
	return structResult(st, sb.String()), nil
}

// OrdenDetalle es el detalle de una orden de compra (cuando se consulta por código).
type OrdenDetalle struct {
	MPItem
	Tipo             string      `json:"tipo"`
	Organismo        MPOrganismo `json:"organismo"`
	Proveedor        *MPProv     `json:"proveedor,omitempty"`
	TotalNeto        float64     `json:"total_neto"`
	Total            float64     `json:"total"`
	Moneda           string      `json:"moneda"`
	FechaCreacion    string      `json:"fecha_creacion,omitempty"`
	FechaEnvio       string      `json:"fecha_envio,omitempty"`
	FechaAceptacion  string      `json:"fecha_aceptacion,omitempty"`
	CodigoLicitacion string      `json:"codigo_licitacion,omitempty"`
	Descripcion      string      `json:"descripcion"`
}

// MPProv es el proveedor de una orden de compra.
type MPProv struct {
	Codigo    string `json:"codigo"`
	Nombre    string `json:"nombre"`
	Rut       string `json:"rut"`
	Actividad string `json:"actividad,omitempty"`
}

// OrdenesOut es la salida estructurada de ordenes_compra.
type OrdenesOut struct {
	Total   int           `json:"total"`
	Pagina  int           `json:"pagina"`
	Limite  int           `json:"limite"`
	Ordenes []MPItem      `json:"ordenes" jsonschema:"description=Filas de esta página"`
	Detalle *OrdenDetalle `json:"detalle,omitempty" jsonschema:"description=Presente cuando la búsqueda devuelve una sola orden con detalle (por codigo)"`
}

func (h *handlers) buscarOrdenesCompra(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !h.MP.TieneTicket() {
		return errMsg(mp.SinTicket), nil
	}
	params, res := mpParams(req, mp.EstadosOC)
	if res != nil {
		return res, nil
	}
	limite := limitArg(req, "limite", 25, 100)
	pagina := offsetArg(req, "pagina", 1, 1, 100_000)

	st := OrdenesOut{Pagina: pagina, Limite: limite, Ordenes: []MPItem{}}
	listado, err := h.MP.Ordenes(ctx, params)
	if mp.NoResults(err) {
		return structResult(st, "No se encontraron órdenes de compra con esos filtros."), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	total := len(listado)
	st.Total = total
	if total == 0 {
		return structResult(st, "No se encontraron órdenes de compra con esos filtros."), nil
	}

	item := func(o mp.Orden) MPItem {
		return MPItem{Codigo: o.Codigo, Nombre: text.CleanText(o.Nombre), Estado: o.EstadoTexto()}
	}
	var sb strings.Builder
	if total == 1 && listado[0].Comprador != nil {
		o := listado[0]
		c := o.Comprador
		d := &OrdenDetalle{MPItem: item(o), Tipo: o.Tipo, TotalNeto: o.TotalNeto, Total: o.Total, Moneda: o.TipoMoneda,
			CodigoLicitacion: o.CodigoLicitacion, Descripcion: text.CleanText(o.Descripcion),
			Organismo: MPOrganismo{Codigo: c.CodigoOrganismo, Nombre: text.CleanText(c.NombreOrganismo), Unidad: text.CleanText(c.NombreUnidad), Region: text.CleanText(c.RegionUnidad)}}
		fmt.Fprintf(&sb, "## %s\n", text.CleanText(o.Nombre))
		fmt.Fprintf(&sb, "- Código: **%s** | Tipo: %s | Estado: %s\n", o.Codigo, o.Tipo, o.EstadoTexto())
		fmt.Fprintf(&sb, "- Organismo: %s (código %s) — %s, %s\n", text.CleanText(c.NombreOrganismo), c.CodigoOrganismo, text.CleanText(c.NombreUnidad), text.CleanText(c.RegionUnidad))
		if p := o.Proveedor; p != nil {
			d.Proveedor = &MPProv{p.Codigo, text.CleanText(p.Nombre), p.RutSucursal, text.CleanText(p.Actividad)}
			fmt.Fprintf(&sb, "- Proveedor: %s (código %s, RUT %s) — %s\n", text.CleanText(p.Nombre), p.Codigo, p.RutSucursal, text.CleanText(p.Actividad))
		}
		fmt.Fprintf(&sb, "- Total neto: %s | Total: %s\n", pesos(o.TotalNeto, o.TipoMoneda), pesos(o.Total, o.TipoMoneda))
		if f := o.Fechas; f != nil {
			d.FechaCreacion, d.FechaEnvio, d.FechaAceptacion = fechaMP(f.FechaCreacion), fechaMP(f.FechaEnvio), fechaMP(f.FechaAceptacion)
			fmt.Fprintf(&sb, "- Creación: %s | Envío: %s | Aceptación: %s\n", fechaMP(f.FechaCreacion), fechaMP(f.FechaEnvio), fechaMP(f.FechaAceptacion))
		}
		if o.CodigoLicitacion != "" {
			fmt.Fprintf(&sb, "- Licitación asociada: %s\n", o.CodigoLicitacion)
		}
		fmt.Fprintf(&sb, "- Descripción: %s\n", text.Truncate(text.CleanText(o.Descripcion), 1000))
		st.Ordenes = append(st.Ordenes, item(o))
		st.Pagina, st.Detalle = 1, d
		return structResult(st, sb.String()), nil
	}

	from, to, nota := mp.Pagina(total, limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d órdenes (%d páginas de %d).", pagina, total, (total+limite-1)/limite, limite), nil
	}
	fmt.Fprintf(&sb, "## Órdenes de compra — %d resultados\n%s\nPara organismo, proveedor y montos de una orden, consultá con codigo=<Código>.\n\n", total, nota)
	sb.WriteString(text.MDHeader("Código", "Nombre", "Estado"))
	for _, o := range listado[from:to] {
		sb.WriteString(text.MDRow(o.Codigo, text.Cell(o.Nombre, 100), o.EstadoTexto()))
		st.Ordenes = append(st.Ordenes, item(o))
	}
	return structResult(st, sb.String()), nil
}

func (h *handlers) buscarProveedor(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !h.MP.TieneTicket() {
		return errMsg(mp.SinTicket), nil
	}
	rut, err := text.NormRUT(stringArg(req, "rut"))
	if err != nil {
		return errResult(err), nil
	}
	lista, err := h.MP.BuscarProveedor(ctx, text.RutConPuntos(rut)) // la API solo encuentra el RUT con puntos
	if mp.NoResults(err) {
		return textResult(fmt.Sprintf("No hay un proveedor del Estado registrado con RUT %s en Mercado Público.", rut)), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	if len(lista) == 0 {
		return textResult(fmt.Sprintf("No hay un proveedor del Estado registrado con RUT %s en Mercado Público.", rut)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Proveedor del Estado — RUT %s\n\n", rut)
	sb.WriteString(text.MDHeader("Código proveedor", "Nombre"))
	for _, e := range lista {
		sb.WriteString(text.MDRow(e.CodigoEmpresa, text.Cell(e.NombreEmpresa, 100)))
	}
	sb.WriteString("\nUsá el código en licitaciones(codigo_proveedor=...) u ordenes_compra(codigo_proveedor=..., fecha=...).\n")
	return textResult(sb.String()), nil
}

func (h *handlers) listarOrganismos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if !h.MP.TieneTicket() {
		return errMsg(mp.SinTicket), nil
	}
	q := text.NormText(stringArg(req, "q"))
	limite := limitArg(req, "limite", 50, 500)
	pagina := offsetArg(req, "pagina", 1, 1, 10_000)

	orgs, err := h.MP.Organismos(ctx)
	if err != nil {
		return errResult(err), nil
	}

	var match []mp.Empresa
	for _, o := range orgs {
		if q == "" || strings.Contains(text.NormText(o.NombreEmpresa), q) || o.CodigoEmpresa == q {
			match = append(match, o)
		}
	}
	if len(match) == 0 {
		return textResult(fmt.Sprintf("Ningún organismo de los %d coincide con %q. Probá con una palabra (ej: 'salud', 'municipalidad de temuco').", len(orgs), stringArg(req, "q"))), nil
	}
	from, to, nota := mp.Pagina(len(match), limite, pagina)
	if to == 0 {
		return errMsg("pagina %d fuera de rango: hay %d organismos.", pagina, len(match)), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Organismos públicos en Mercado Público — %d coinciden (de %d)\n%s\n\n", len(match), len(orgs), nota)
	sb.WriteString(text.MDHeader("Código", "Nombre"))
	for _, o := range match[from:to] {
		sb.WriteString(text.MDRow(o.CodigoEmpresa, text.Cell(o.NombreEmpresa, 100)))
	}
	sb.WriteString("\nUsá el código en licitaciones(codigo_organismo=...) u ordenes_compra(codigo_organismo=...).\n")
	return textResult(sb.String()), nil
}
