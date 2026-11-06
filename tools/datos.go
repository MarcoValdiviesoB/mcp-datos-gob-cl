package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/ckan"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== buscar_datasets =====================

var slugRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

func (h *handlers) buscarDatasets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q := stringArg(req, "q")
	if q == "" {
		q = "*:*"
	}
	organismo := strings.ToLower(stringArg(req, "organismo"))
	limite := limitArg(req, "limite", 10, 100)
	offset := offsetArg(req, "offset", 0, 0, 100_000)

	params := url.Values{"q": {q}, "rows": {strconv.Itoa(limite)}, "start": {strconv.Itoa(offset)}}
	if organismo != "" {
		if !slugRe.MatchString(organismo) {
			return errMsg("organismo %q no es un slug válido (ej: 'direccion_de_presupuestos'). Usá categorias_datasets para ver los slugs.", organismo), nil
		}
		params.Set("fq", fmt.Sprintf("organization:%q", organismo))
	}

	resp, err := h.CKAN.PackageSearch(ctx, params)
	if err != nil {
		return errResult(err), nil
	}

	total := resp.Count
	if total == 0 {
		if organismo != "" {
			return textResult(fmt.Sprintf("No hay datasets para organismo=%q con esa búsqueda. Si el slug no existe, usá categorias_datasets para ver los slugs válidos.", organismo)), nil
		}
		return textResult("No se encontraron datasets. Probá con otras palabras o sin tildes."), nil
	}
	if offset >= total {
		return errMsg("offset %d fuera de rango: hay %d datasets (offset válido: 0 a %d).", offset, total, total-1), nil
	}

	var blocks []string
	for _, ds := range resp.Results {
		var b strings.Builder
		fmt.Fprintf(&b, "## %s\n", text.CleanText(ds.Title))
		fmt.Fprintf(&b, "dataset: `%s` | organismo: %s (`%s`)\n", ds.Name, text.CleanText(ds.Organization.Title), ds.Organization.Name)
		if notes := text.Truncate(text.CleanText(ds.Notes), 200); notes != "" {
			fmt.Fprintf(&b, "Descripción: %s\n", notes)
		}
		const maxRes = 8
		for i, r := range ds.Resources {
			if i == maxRes {
				fmt.Fprintf(&b, "  (+%d recursos más)\n", len(ds.Resources)-maxRes)
				break
			}
			name := text.Truncate(text.CleanText(r.Name), 80)
			if r.DatastoreActive {
				fmt.Fprintf(&b, "  -> %s [%s] resource_id: %s\n", name, r.Format, r.ID)
			} else {
				fmt.Fprintf(&b, "  -> %s [%s] sin DataStore (solo descarga): %s\n", name, r.Format, r.URL)
			}
		}
		b.WriteString("\n")
		blocks = append(blocks, b.String())
	}
	out, n := text.FitBlocks(blocks, text.MaxOutput-600)
	hasta := offset + n

	var sb strings.Builder
	fmt.Fprintf(&sb, "Datasets %d-%d de %d en total.\n", offset+1, hasta, total)
	if hasta < total {
		fmt.Fprintf(&sb, "(para más, repetí con offset=%d)\n", hasta)
	}
	sb.WriteString("Los recursos con resource_id se consultan con ver_recurso/consultar; los 'sin DataStore' solo se descargan.\n\n")
	sb.WriteString(out)
	return textResult(sb.String()), nil
}

// ===================== categorias_datasets =====================

func (h *handlers) categoriasDatasets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limite := limitArg(req, "limite", 30, 200)
	resp, err := h.CKAN.PackageFacets(ctx, `["organization","tags"]`, limite)
	if err != nil {
		return errResult(err), nil
	}
	sorted := func(items []ckan.FacetItem) []ckan.FacetItem {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Count != items[j].Count {
				return items[i].Count > items[j].Count
			}
			return items[i].Name < items[j].Name
		})
		return items
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Mapa de datos.gob.cl — %d datasets\n\n", resp.Count)
	fmt.Fprintf(&sb, "## Organismos con más datasets (top %d)\n", limite)
	for _, it := range sorted(resp.Items("organization")) {
		fmt.Fprintf(&sb, "- `%s` — %s — %d datasets\n", it.Name, text.CleanText(it.DisplayName), it.Count)
	}
	fmt.Fprintf(&sb, "\n## Tags más frecuentes (top %d)\n", limite)
	for _, it := range sorted(resp.Items("tags")) {
		fmt.Fprintf(&sb, "- %s — %d\n", text.CleanText(it.DisplayName), it.Count)
	}
	sb.WriteString("\nFiltrá por organismo con buscar_datasets(organismo=\"<slug>\"). Para ver más organismos usá limite (máx 200).\n")
	return textResult(sb.String()), nil
}

// ===================== ver_recurso / consultar =====================

// explicarRecurso se usa cuando datastore_search da 404: distingue "no existe" de "existe pero sin DataStore".
func (h *handlers) explicarRecurso(ctx context.Context, rid string) *mcp.CallToolResult {
	r, err := h.CKAN.ResourceShow(ctx, rid)
	if ckan.IsNotFound(err) {
		return errMsg("No existe un recurso con id %q. Buscá ids válidos (resource_id) con buscar_datasets.", rid)
	}
	if errors.Is(err, ckan.ErrDecode) {
		return errMsg("No se pudo leer el recurso %q.", rid)
	}
	if err != nil {
		return errResult(err)
	}
	if !r.DatastoreActive {
		return errMsg("El recurso %q (%s, formato %s) no tiene DataStore: no se puede consultar por API. Descargalo desde %s",
			rid, text.Truncate(text.CleanText(r.Name), 80), r.Format, r.URL)
	}
	return errMsg("El recurso %q existe pero su DataStore no respondió. Reintentá en unos minutos.", rid)
}

func (h *handlers) dsSearch(ctx context.Context, rid string, params url.Values) (*ckan.DSResult, *mcp.CallToolResult) {
	resp, err := h.CKAN.DatastoreSearch(ctx, rid, params)
	if ckan.IsNotFound(err) {
		return nil, h.explicarRecurso(ctx, rid)
	}
	if err != nil {
		return nil, errResult(err)
	}
	return resp, nil
}

const notaTipos = "Nota: los filtros comparan como texto exacto (valores como string). En campos 'text', ordenar es alfabético (\"10\" < \"9\")."

func (h *handlers) verRecurso(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rid := stringArg(req, "resource_id")
	if rid == "" {
		return errMsg("falta resource_id (obtenelo con buscar_datasets)"), nil
	}
	muestra := offsetArg(req, "muestra", 3, 0, 50)

	resp, res := h.dsSearch(ctx, rid, url.Values{"limit": {strconv.Itoa(muestra)}})
	if res != nil {
		return res, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Total registros: %d\n\n### Campos\n", resp.Total)
	var cols []string
	for _, f := range resp.Fields {
		if ckan.InternalField(f.ID) {
			continue
		}
		cols = append(cols, f.ID)
		fmt.Fprintf(&sb, "- **%s** (%s)\n", f.ID, f.Type)
	}
	sb.WriteString("\n" + notaTipos + "\n")
	if muestra > 0 && len(resp.Records) > 0 {
		sb.WriteString("\n### Muestra\n")
		sb.WriteString(recordsTable(cols, resp.Records, text.MaxOutput-sb.Len()-200))
	}
	return textResult(sb.String()), nil
}

// recordsTable arma una tabla markdown compacta; si no caben todas las filas, avisa cuántas se muestran.
func recordsTable(cols []string, records []map[string]any, budget int) string {
	rows := make([]string, 0, len(records))
	for _, rec := range records {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = text.Cell(rec[c], 60)
		}
		rows = append(rows, text.MDRow(cells...))
	}
	head := text.MDHeader(cols...)
	out, n := text.FitBlocks(rows, budget-len(head))
	out = head + out
	if n < len(rows) {
		out += fmt.Sprintf("\n(se muestran %d de %d filas por tamaño; usá 'campos' o un limite menor)\n", n, len(rows))
	}
	return out
}

// ConsultaOut es la salida estructurada de consultar.
type ConsultaOut struct {
	ResourceID      string           `json:"resource_id"`
	Total           int              `json:"total" jsonschema:"description=Registros que cumplen la consulta"`
	Offset          int              `json:"offset"`
	Campos          []string         `json:"campos"`
	Registros       []map[string]any `json:"registros" jsonschema:"description=Registros de esta página (los mismos que la tabla de texto)"`
	SiguienteOffset *int             `json:"siguiente_offset,omitempty" jsonschema:"description=Offset para la página siguiente; ausente si no hay más"`
	Recortado       bool             `json:"recortado" jsonschema:"description=true si se devolvieron menos registros que los pedidos por tamaño"`
}

func (h *handlers) consultar(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rid := stringArg(req, "resource_id")
	if rid == "" {
		return errMsg("falta resource_id (obtenelo con buscar_datasets)"), nil
	}
	q := stringArg(req, "q")
	filtros := stringArg(req, "filtros")
	campos := stringArg(req, "campos")
	ordenar := stringArg(req, "ordenar")
	limite := limitArg(req, "limite", 20, 500)
	offset := offsetArg(req, "offset", 0, 0, 10_000_000)

	params := url.Values{"limit": {strconv.Itoa(limite)}}
	if q != "" {
		params.Set("q", q)
	}
	if filtros != "" {
		f, err := ckan.StringifyFilters(filtros)
		if err != nil {
			return errResult(err), nil
		}
		params.Set("filters", f)
	}
	if campos != "" {
		parts := strings.Split(campos, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		params.Set("fields", strings.Join(parts, ","))
	}
	if ordenar != "" {
		params.Set("sort", ordenar)
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}

	resp, res := h.dsSearch(ctx, rid, params)
	if res != nil {
		return res, nil
	}
	total := resp.Total
	if total > 0 && offset >= total {
		return errMsg("offset %d fuera de rango: la consulta tiene %d registros (offset válido: 0 a %d).", offset, total, total-1), nil
	}

	cols := resp.Columns()
	rows := make([]string, 0, len(resp.Records))
	for _, rec := range resp.Records {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = text.Cell(rec[c], 60)
		}
		rows = append(rows, text.MDRow(cells...))
	}
	head := text.MDHeader(cols...)
	out, n := text.FitBlocks(rows, text.MaxOutput-len(head)-500)

	st := ConsultaOut{ResourceID: rid, Total: total, Offset: offset, Campos: cols, Registros: make([]map[string]any, 0, n), Recortado: n < len(rows)}
	if st.Campos == nil {
		st.Campos = []string{}
	}
	for _, rec := range resp.Records[:n] {
		r := make(map[string]any, len(cols))
		for _, c := range cols {
			r[c] = rec[c]
		}
		st.Registros = append(st.Registros, r)
	}

	var sb strings.Builder
	if n == 0 {
		fmt.Fprintf(&sb, "Total: %d | Sin registros para esta consulta.\n", total)
		return structResult(st, sb.String()), nil
	}
	fmt.Fprintf(&sb, "Total: %d | Mostrando %d-%d", total, offset+1, offset+n)
	if n < len(rows) {
		fmt.Fprintf(&sb, " (recortado por tamaño: %d de %d pedidos; usá 'campos' para traer menos columnas)", n, len(rows))
	}
	sb.WriteString("\n")
	if offset+n < total {
		sig := offset + n
		st.SiguienteOffset = &sig
		fmt.Fprintf(&sb, "Siguiente página: offset=%d\n", sig)
	}
	sb.WriteString("\n" + head + out)
	return structResult(st, sb.String()), nil
}

// ===================== buscar_empresa =====================

var anioFmt = regexp.MustCompile(`^\d{4}$`)

func (h *handlers) buscarEmpresa(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rutIn := stringArg(req, "rut")
	nombre := stringArg(req, "nombre")
	anio := stringArg(req, "anio")
	limite := limitArg(req, "limite", 10, 100)
	offset := offsetArg(req, "offset", 0, 0, 900)

	if rutIn == "" && nombre == "" {
		return errMsg("debes especificar rut o nombre"), nil
	}
	var rut string
	if rutIn != "" {
		r, err := text.NormRUT(rutIn)
		if err != nil {
			return errResult(err), nil
		}
		rut = r
	}
	if anio != "" && !anioFmt.MatchString(anio) {
		return errMsg("anio debe tener 4 dígitos, ej: 2025"), nil
	}

	ea := h.CKAN.RecursosEmpresas(ctx)
	activos := ckan.SortedYears(ea.Activos)
	years := activos
	if anio != "" {
		if _, ok := ea.Activos[anio]; !ok {
			if u, ok := ea.Descargas[anio]; ok {
				return errMsg("El registro %s existe solo como archivo descargable (sin DataStore), no se puede consultar por API: %s. Años consultables: %s.", anio, u, strings.Join(activos, ", ")), nil
			}
			return errMsg("No hay registro para el año %s. Años consultables: %s.", anio, strings.Join(activos, ", ")), nil
		}
		years = []string{anio}
	}

	params := url.Values{"limit": {strconv.Itoa(offset + limite)}}
	if rut != "" {
		f, _ := json.Marshal(map[string]string{"RUT": rut})
		params.Set("filters", string(f))
	} else {
		qq, _ := json.Marshal(map[string]string{"Razon Social": nombre})
		params.Set("q", string(qq))
	}

	type res struct {
		anio string
		resp *ckan.DSResult
		err  *mcp.CallToolResult
	}
	results := make([]res, len(years))
	var wg sync.WaitGroup
	for i, y := range years {
		wg.Add(1)
		go func(i int, y string) {
			defer wg.Done()
			p := url.Values{}
			for k, v := range params {
				p[k] = v
			}
			r, e := h.dsSearch(ctx, ea.Activos[y], p)
			results[i] = res{y, r, e}
		}(i, y)
	}
	wg.Wait()

	total := 0
	var fallidos []string
	type fila struct {
		anio string
		rec  map[string]any
	}
	var filas []fila
	for _, r := range results {
		if r.err != nil {
			fallidos = append(fallidos, r.anio)
			continue
		}
		total += r.resp.Total
		for _, rec := range r.resp.Records {
			filas = append(filas, fila{r.anio, rec})
		}
	}
	if len(fallidos) == len(years) {
		return results[0].err, nil
	}

	var sb strings.Builder
	quien := "nombre " + strconv.Quote(nombre)
	if rut != "" {
		quien = "RUT " + rut
	}
	if total == 0 {
		fmt.Fprintf(&sb, "No se encontraron empresas con %s en los registros %s.\n", quien, strings.Join(years, ", "))
	} else {
		if offset >= len(filas) {
			return errMsg("offset %d fuera de rango: hay %d resultados.", offset, total), nil
		}
		page := filas[offset:min(len(filas), offset+limite)]
		fmt.Fprintf(&sb, "Encontradas: %d empresas con %s (registros %s). Mostrando %d-%d.\n", total, quien, strings.Join(years, ", "), offset+1, offset+len(page))
		if offset+len(page) < total {
			fmt.Fprintf(&sb, "Siguiente página: offset=%d\n", offset+len(page))
		}
		sb.WriteString("\n" + text.MDHeader("Razón social", "RUT", "Tipo", "Capital", "Comuna (región)", "Constitución", "Aprobación SII", "Actuación", "Registro"))
		for _, f := range page {
			r := f.rec
			sb.WriteString(text.MDRow(text.Cell(r["Razon Social"], 60), text.Cell(r["RUT"], 12), text.Cell(r["Codigo de sociedad"], 10),
				"$"+text.Cell(r["Capital"], 16), fmt.Sprintf("%s (%s)", text.Cell(r["Comuna Tributaria"], 30), text.Cell(r["Region Tributaria"], 4)),
				text.Cell(r["Fecha de actuacion (1era firma)"], 10), text.Cell(r["Fecha de aprobacion x SII"], 10), text.Cell(r["Tipo de actuacion"], 20), f.anio))
		}
	}
	if len(fallidos) > 0 {
		fmt.Fprintf(&sb, "\nAviso: no se pudo consultar el registro de %s (error de la API).\n", strings.Join(fallidos, ", "))
	}
	if anio == "" && len(ea.Descargas) > 0 {
		fmt.Fprintf(&sb, "\nNota: el registro solo contiene constituciones/modificaciones por año. Años %s están solo como CSV (sin DataStore) y no se consultan por API.\n", ckan.RangoAnios(ckan.SortedYears(ea.Descargas)))
	}
	return textResult(sb.String()), nil
}
