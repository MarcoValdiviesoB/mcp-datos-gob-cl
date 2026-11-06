package main

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
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

const apiBase = "https://datos.gob.cl/api/action"

// ===================== Cliente CKAN =====================

// ckanError es un error devuelto por CKAN ({"success":false,"error":{...}}), ya traducido.
type ckanError struct {
	tipo   string // "Not Found Error", "Validation Error", "Search Error", ...
	msg    string // mensaje en español para el LLM
	campos map[string]string
}

func (e *ckanError) Error() string { return e.msg }

func (e *ckanError) notFound() bool { return e.tipo == "Not Found Error" }

// nombres de parámetros de CKAN -> parámetros de las tools.
var ckanParam = map[string]string{"sort": "ordenar", "filters": "filtros", "fields": "campos", "q": "q", "limit": "limite", "offset": "offset", "resource_id": "resource_id"}

// parseCKANError traduce el cuerpo de error de CKAN a una frase útil. Devuelve nil si no es un error de CKAN.
func parseCKANError(body []byte) *ckanError {
	var r struct {
		Success *bool                      `json:"success"`
		Error   map[string]json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &r) != nil || r.Error == nil {
		return nil
	}
	e := &ckanError{campos: map[string]string{}}
	_ = json.Unmarshal(r.Error["__type"], &e.tipo)
	var raw string
	_ = json.Unmarshal(r.Error["message"], &raw)
	for k, v := range r.Error {
		if k == "__type" || k == "message" || k == "info" {
			continue
		}
		var l []string
		if json.Unmarshal(v, &l) == nil && len(l) > 0 {
			e.campos[k] = strings.Join(l, "; ")
		}
	}
	switch {
	case e.tipo == "Not Found Error":
		e.msg = "No encontrado en datos.gob.cl: " + strings.TrimPrefix(raw, "No encontrado: ")
	case e.tipo == "Search Error":
		e.msg = "La búsqueda tiene sintaxis inválida (caracteres como : \" ( ) [ ] * son operadores). Probá con palabras simples."
	case e.campos["query"] != "":
		e.msg = "Consulta inválida para DataStore. Lo más común: un filtro numérico contra un campo de texto (los filtros van como string), o un campo de 'ordenar' mal escrito. Revisá campos y tipos con ver_recurso."
	case len(e.campos) > 0:
		keys := make([]string, 0, len(e.campos))
		for k := range e.campos {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			name := k
			if p, ok := ckanParam[k]; ok {
				name = p
			}
			parts = append(parts, fmt.Sprintf("%s: %s", name, strings.ReplaceAll(e.campos[k], "invalid value", "valor inválido")))
		}
		e.msg = "Parámetro inválido — " + strings.Join(parts, "; ") + ". Revisá los nombres exactos de los campos con ver_recurso (distinguen mayúsculas y tildes)."
	case raw != "":
		e.msg = "datos.gob.cl respondió con error: " + raw
	default:
		e.msg = "datos.gob.cl respondió con un error (" + e.tipo + ")"
	}
	return e
}

// apiGet llama a la API CKAN de datos.gob.cl y traduce sus errores.
func apiGet(ctx context.Context, action string, params url.Values) ([]byte, error) {
	body, err := doGet(ctx, fuenteCKAN, apiBase+"/"+action+"?"+params.Encode())
	var he *httpError
	if errors.As(err, &he) {
		if ce := parseCKANError(he.body); ce != nil {
			return nil, ce
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if ce := parseCKANError(body); ce != nil {
		return nil, ce
	}
	return body, nil
}

type dsResponse struct {
	Result struct {
		Total   int              `json:"total"`
		Fields  []dsField        `json:"fields"`
		Records []map[string]any `json:"records"`
	} `json:"result"`
}

type dsField struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

func internalField(id string) bool { return id == "_id" || id == "_full_text" || id == "rank" }

type ckanResource struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Format          string `json:"format"`
	URL             string `json:"url"`
	DatastoreActive bool   `json:"datastore_active"`
	PackageID       string `json:"package_id"`
}

// ===================== buscar_datasets =====================

var slugRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

func buscarDatasets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	body, err := apiGet(ctx, "package_search", params)
	if err != nil {
		return errResult(err), nil
	}

	var resp struct {
		Result struct {
			Count   int `json:"count"`
			Results []struct {
				Title        string `json:"title"`
				Name         string `json:"name"`
				Notes        string `json:"notes"`
				Organization struct {
					Name  string `json:"name"`
					Title string `json:"title"`
				} `json:"organization"`
				Resources []ckanResource `json:"resources"`
			} `json:"results"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de datos.gob.cl: %v", err), nil
	}

	total := resp.Result.Count
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
	for _, ds := range resp.Result.Results {
		var b strings.Builder
		fmt.Fprintf(&b, "## %s\n", cleanText(ds.Title))
		fmt.Fprintf(&b, "dataset: `%s` | organismo: %s (`%s`)\n", ds.Name, cleanText(ds.Organization.Title), ds.Organization.Name)
		if notes := truncate(cleanText(ds.Notes), 200); notes != "" {
			fmt.Fprintf(&b, "Descripción: %s\n", notes)
		}
		const maxRes = 8
		for i, r := range ds.Resources {
			if i == maxRes {
				fmt.Fprintf(&b, "  (+%d recursos más)\n", len(ds.Resources)-maxRes)
				break
			}
			name := truncate(cleanText(r.Name), 80)
			if r.DatastoreActive {
				fmt.Fprintf(&b, "  -> %s [%s] resource_id: %s\n", name, r.Format, r.ID)
			} else {
				fmt.Fprintf(&b, "  -> %s [%s] sin DataStore (solo descarga): %s\n", name, r.Format, r.URL)
			}
		}
		b.WriteString("\n")
		blocks = append(blocks, b.String())
	}
	text, n := fitBlocks(blocks, maxOutput-600)
	hasta := offset + n

	var sb strings.Builder
	fmt.Fprintf(&sb, "Datasets %d-%d de %d en total.\n", offset+1, hasta, total)
	if hasta < total {
		fmt.Fprintf(&sb, "(para más, repetí con offset=%d)\n", hasta)
	}
	sb.WriteString("Los recursos con resource_id se consultan con ver_recurso/consultar; los 'sin DataStore' solo se descargan.\n\n")
	sb.WriteString(text)
	return textResult(sb.String()), nil
}

// ===================== categorias_datasets =====================

func categoriasDatasets(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	limite := limitArg(req, "limite", 30, 200)
	params := url.Values{
		"q":           {"*:*"},
		"rows":        {"0"},
		"facet.field": {`["organization","tags"]`},
		"facet.limit": {strconv.Itoa(limite)},
	}
	body, err := apiGet(ctx, "package_search", params)
	if err != nil {
		return errResult(err), nil
	}
	type item struct {
		Name        string `json:"name"`
		DisplayName string `json:"display_name"`
		Count       int    `json:"count"`
	}
	var resp struct {
		Result struct {
			Count        int `json:"count"`
			SearchFacets map[string]struct {
				Items []item `json:"items"`
			} `json:"search_facets"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de datos.gob.cl: %v", err), nil
	}
	sorted := func(items []item) []item {
		sort.Slice(items, func(i, j int) bool {
			if items[i].Count != items[j].Count {
				return items[i].Count > items[j].Count
			}
			return items[i].Name < items[j].Name
		})
		return items
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Mapa de datos.gob.cl — %d datasets\n\n", resp.Result.Count)
	fmt.Fprintf(&sb, "## Organismos con más datasets (top %d)\n", limite)
	for _, it := range sorted(resp.Result.SearchFacets["organization"].Items) {
		fmt.Fprintf(&sb, "- `%s` — %s — %d datasets\n", it.Name, cleanText(it.DisplayName), it.Count)
	}
	fmt.Fprintf(&sb, "\n## Tags más frecuentes (top %d)\n", limite)
	for _, it := range sorted(resp.Result.SearchFacets["tags"].Items) {
		fmt.Fprintf(&sb, "- %s — %d\n", cleanText(it.DisplayName), it.Count)
	}
	sb.WriteString("\nFiltrá por organismo con buscar_datasets(organismo=\"<slug>\"). Para ver más organismos usá limite (máx 200).\n")
	return textResult(sb.String()), nil
}

// ===================== ver_recurso / consultar =====================

// explicarRecurso se usa cuando datastore_search da 404: distingue "no existe" de "existe pero sin DataStore".
func explicarRecurso(ctx context.Context, rid string) *mcp.CallToolResult {
	body, err := apiGet(ctx, "resource_show", url.Values{"id": {rid}})
	var ce *ckanError
	if errors.As(err, &ce) && ce.notFound() {
		return errMsg("No existe un recurso con id %q. Buscá ids válidos (resource_id) con buscar_datasets.", rid)
	}
	if err != nil {
		return errResult(err)
	}
	var r struct {
		Result ckanResource `json:"result"`
	}
	if json.Unmarshal(body, &r) != nil {
		return errMsg("No se pudo leer el recurso %q.", rid)
	}
	if !r.Result.DatastoreActive {
		return errMsg("El recurso %q (%s, formato %s) no tiene DataStore: no se puede consultar por API. Descargalo desde %s",
			rid, truncate(cleanText(r.Result.Name), 80), r.Result.Format, r.Result.URL)
	}
	return errMsg("El recurso %q existe pero su DataStore no respondió. Reintentá en unos minutos.", rid)
}

func dsSearch(ctx context.Context, rid string, params url.Values) (*dsResponse, *mcp.CallToolResult) {
	params.Set("resource_id", rid)
	body, err := apiGet(ctx, "datastore_search", params)
	var ce *ckanError
	if errors.As(err, &ce) && ce.notFound() {
		return nil, explicarRecurso(ctx, rid)
	}
	if err != nil {
		return nil, errResult(err)
	}
	var resp dsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errMsg("respuesta inesperada de datos.gob.cl: %v", err)
	}
	return &resp, nil
}

const notaTipos = "Nota: los filtros comparan como texto exacto (valores como string). En campos 'text', ordenar es alfabético (\"10\" < \"9\")."

func verRecurso(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rid := stringArg(req, "resource_id")
	if rid == "" {
		return errMsg("falta resource_id (obtenelo con buscar_datasets)"), nil
	}
	muestra := offsetArg(req, "muestra", 3, 0, 50)

	resp, res := dsSearch(ctx, rid, url.Values{"limit": {strconv.Itoa(muestra)}})
	if res != nil {
		return res, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Total registros: %d\n\n### Campos\n", resp.Result.Total)
	var cols []string
	for _, f := range resp.Result.Fields {
		if internalField(f.ID) {
			continue
		}
		cols = append(cols, f.ID)
		fmt.Fprintf(&sb, "- **%s** (%s)\n", f.ID, f.Type)
	}
	sb.WriteString("\n" + notaTipos + "\n")
	if muestra > 0 && len(resp.Result.Records) > 0 {
		sb.WriteString("\n### Muestra\n")
		sb.WriteString(recordsTable(cols, resp.Result.Records, maxOutput-sb.Len()-200))
	}
	return textResult(sb.String()), nil
}

// recordsTable arma una tabla markdown compacta; si no caben todas las filas, avisa cuántas se muestran.
func recordsTable(cols []string, records []map[string]any, budget int) string {
	rows := make([]string, 0, len(records))
	for _, rec := range records {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(rec[c], 60)
		}
		rows = append(rows, mdRow(cells...))
	}
	head := mdHeader(cols...)
	text, n := fitBlocks(rows, budget-len(head))
	out := head + text
	if n < len(rows) {
		out += fmt.Sprintf("\n(se muestran %d de %d filas por tamaño; usá 'campos' o un limite menor)\n", n, len(rows))
	}
	return out
}

func consultar(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		f, err := stringifyFilters(filtros)
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

	resp, res := dsSearch(ctx, rid, params)
	if res != nil {
		return res, nil
	}
	total := resp.Result.Total
	if total > 0 && offset >= total {
		return errMsg("offset %d fuera de rango: la consulta tiene %d registros (offset válido: 0 a %d).", offset, total, total-1), nil
	}

	var cols []string
	for _, f := range resp.Result.Fields {
		if !internalField(f.ID) {
			cols = append(cols, f.ID)
		}
	}
	rows := make([]string, 0, len(resp.Result.Records))
	for _, rec := range resp.Result.Records {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = cell(rec[c], 60)
		}
		rows = append(rows, mdRow(cells...))
	}
	head := mdHeader(cols...)
	text, n := fitBlocks(rows, maxOutput-len(head)-500)

	var sb strings.Builder
	if n == 0 {
		fmt.Fprintf(&sb, "Total: %d | Sin registros para esta consulta.\n", total)
		return textResult(sb.String()), nil
	}
	fmt.Fprintf(&sb, "Total: %d | Mostrando %d-%d", total, offset+1, offset+n)
	if n < len(rows) {
		fmt.Fprintf(&sb, " (recortado por tamaño: %d de %d pedidos; usá 'campos' para traer menos columnas)", n, len(rows))
	}
	sb.WriteString("\n")
	if offset+n < total {
		fmt.Fprintf(&sb, "Siguiente página: offset=%d\n", offset+n)
	}
	sb.WriteString("\n" + head + text)
	return textResult(sb.String()), nil
}

// ===================== buscar_empresa =====================

const empresasDataset = "registro-de-empresas-y-sociedades"

// empresaFallback: recursos con DataStore conocidos (2026-11), por si package_show falla.
var empresaFallback = map[string]string{
	"2026": "472de7b5-384f-452d-9da5-2928689d8f2f",
	"2025": "71c8e355-226a-461e-809a-870c2275a178",
	"2024": "42ee8c8c-59cf-42e4-89af-ec19a87dbf8d",
	"2013": "fd2b91b0-eb8e-45f1-98d0-1f3316bb6468",
}

type empresaAnios struct {
	activos   map[string]string // año -> resource_id con DataStore
	descargas map[string]string // año -> URL (sin DataStore)
}

var (
	empresaMu     sync.Mutex
	empresaCache  *empresaAnios
	empresaCacheT time.Time
	anioRe        = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

// recursosEmpresas descubre los recursos por año del Registro de Empresas (cache de 6 h, fallback fijo).
func recursosEmpresas(ctx context.Context) *empresaAnios {
	empresaMu.Lock()
	defer empresaMu.Unlock()
	if empresaCache != nil && time.Since(empresaCacheT) < 6*time.Hour {
		return empresaCache
	}
	body, err := apiGet(ctx, "package_show", url.Values{"id": {empresasDataset}})
	if err == nil {
		var r struct {
			Result struct {
				Resources []ckanResource `json:"resources"`
			} `json:"result"`
		}
		if json.Unmarshal(body, &r) == nil && len(r.Result.Resources) > 0 {
			ea := parseEmpresaResources(r.Result.Resources)
			if len(ea.activos) > 0 {
				empresaCache, empresaCacheT = ea, time.Now()
				return ea
			}
		}
	}
	return &empresaAnios{activos: empresaFallback, descargas: map[string]string{}}
}

func parseEmpresaResources(rs []ckanResource) *empresaAnios {
	ea := &empresaAnios{activos: map[string]string{}, descargas: map[string]string{}}
	for _, r := range rs {
		y := anioRe.FindString(r.Name)
		if y == "" {
			continue
		}
		if r.DatastoreActive {
			ea.activos[y] = r.ID
		} else {
			ea.descargas[y] = r.URL
		}
	}
	return ea
}

func sortedYears(m map[string]string) []string {
	ys := make([]string, 0, len(m))
	for y := range m {
		ys = append(ys, y)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ys)))
	return ys
}

var anioFmt = regexp.MustCompile(`^\d{4}$`)

func buscarEmpresa(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		r, err := normRUT(rutIn)
		if err != nil {
			return errResult(err), nil
		}
		rut = r
	}
	if anio != "" && !anioFmt.MatchString(anio) {
		return errMsg("anio debe tener 4 dígitos, ej: 2025"), nil
	}

	ea := recursosEmpresas(ctx)
	activos := sortedYears(ea.activos)
	years := activos
	if anio != "" {
		if _, ok := ea.activos[anio]; !ok {
			if u, ok := ea.descargas[anio]; ok {
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
		resp *dsResponse
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
			r, e := dsSearch(ctx, ea.activos[y], p)
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
		total += r.resp.Result.Total
		for _, rec := range r.resp.Result.Records {
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
		sb.WriteString("\n" + mdHeader("Razón social", "RUT", "Tipo", "Capital", "Comuna (región)", "Constitución", "Aprobación SII", "Actuación", "Registro"))
		for _, f := range page {
			r := f.rec
			sb.WriteString(mdRow(cell(r["Razon Social"], 60), cell(r["RUT"], 12), cell(r["Codigo de sociedad"], 10),
				"$"+cell(r["Capital"], 16), fmt.Sprintf("%s (%s)", cell(r["Comuna Tributaria"], 30), cell(r["Region Tributaria"], 4)),
				cell(r["Fecha de actuacion (1era firma)"], 10), cell(r["Fecha de aprobacion x SII"], 10), cell(r["Tipo de actuacion"], 20), f.anio))
		}
	}
	if len(fallidos) > 0 {
		fmt.Fprintf(&sb, "\nAviso: no se pudo consultar el registro de %s (error de la API).\n", strings.Join(fallidos, ", "))
	}
	if anio == "" && len(ea.descargas) > 0 {
		fmt.Fprintf(&sb, "\nNota: el registro solo contiene constituciones/modificaciones por año. Años %s están solo como CSV (sin DataStore) y no se consultan por API.\n", rangoAnios(sortedYears(ea.descargas)))
	}
	return textResult(sb.String()), nil
}

// rangoAnios compacta años consecutivos: [2023 ... 2014] -> "2014-2023".
func rangoAnios(ys []string) string {
	n := make([]int, 0, len(ys))
	for _, y := range ys {
		if v, err := strconv.Atoi(y); err == nil {
			n = append(n, v)
		}
	}
	sort.Ints(n)
	var parts []string
	for i := 0; i < len(n); {
		j := i
		for j+1 < len(n) && n[j+1] == n[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d-%d", n[i], n[j]))
		} else {
			parts = append(parts, strconv.Itoa(n[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}
