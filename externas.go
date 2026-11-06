package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // America/Santiago aunque el sistema no tenga zoneinfo

	"github.com/mark3labs/mcp-go/mcp"
)

var (
	fuenteMindicador = fuente{"mindicador.cl", 6 * time.Second, 2, 500 * time.Millisecond}
	fuenteFindic     = fuente{"findic.cl", 10 * time.Second, 2, 500 * time.Millisecond}
	fuenteSismosXor  = fuente{"api.xor.cl (CSN)", 8 * time.Second, 2, 500 * time.Millisecond}
	fuenteSismosGael = fuente{"api.gael.cloud", 10 * time.Second, 2, 500 * time.Millisecond}
	fuenteSINCA      = fuente{"SINCA (MMA)", 15 * time.Second, 3, 500 * time.Millisecond}
	fuenteFarmanet   = fuente{"Farmanet (MINSAL)", 15 * time.Second, 3, 500 * time.Millisecond}
)

var santiago = mustLoc("America/Santiago")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("CLT", -3*3600)
	}
	return l
}

// ===================== Indicadores Económicos (mindicador.cl, respaldo findic.cl) =====================

// indicadoresValidos: los que publica mindicador.cl (findic.cl usa los mismos códigos).
var indicadoresValidos = []string{"uf", "ivp", "dolar", "dolar_intercambio", "euro", "ipc", "utm", "imacec", "tpm", "libra_cobre", "tasa_desempleo", "bitcoin"}

var indicadorLabel = map[string]string{
	"uf": "UF", "ivp": "IVP", "dolar": "Dólar observado", "dolar_intercambio": "Dólar acuerdo", "euro": "Euro",
	"ipc": "IPC", "utm": "UTM", "imacec": "IMACEC", "tpm": "TPM", "libra_cobre": "Libra de cobre",
	"tasa_desempleo": "Tasa de desempleo", "bitcoin": "Bitcoin",
}

// getIndicadores intenta mindicador.cl y, si falla, findic.cl (misma API). Devuelve el cuerpo y la fuente usada.
func getIndicadores(ctx context.Context, path string) ([]byte, string, error) {
	body, err := doGet(ctx, fuenteMindicador, "https://mindicador.cl/api"+path)
	if err == nil {
		return body, "mindicador.cl", nil
	}
	body, err2 := doGet(ctx, fuenteFindic, "https://findic.cl/api"+path)
	if err2 == nil {
		return body, "findic.cl (respaldo: mindicador.cl no respondió)", nil
	}
	return nil, "", fmt.Errorf("no se pudieron obtener indicadores: %v; respaldo: %v. Reintentá en unos minutos", err, err2)
}

func indicadoresEconomicos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	indicador := strings.ToLower(stringArg(req, "indicador"))
	anio := stringArg(req, "anio")

	if indicador != "" && !contains(indicadoresValidos, indicador) {
		return errMsg("indicador %q no válido. Usá uno de: %s", indicador, strings.Join(indicadoresValidos, ", ")), nil
	}
	if anio != "" {
		y, err := strconv.Atoi(anio)
		if !anioFmt.MatchString(anio) || err != nil || y < 1928 || y > time.Now().In(santiago).Year() {
			return errMsg("anio debe ser un año de 4 dígitos entre 1928 y el actual, ej: 2025"), nil
		}
		if indicador == "" {
			return errMsg("para una serie histórica indicá también 'indicador' (ej: indicador=uf, anio=%s)", anio), nil
		}
	}

	path := ""
	if indicador != "" {
		path = "/" + url.PathEscape(indicador)
		if anio != "" {
			path += "/" + url.PathEscape(anio)
		}
	}
	body, src, err := getIndicadores(ctx, path)
	if err != nil {
		return errResult(err), nil
	}

	if indicador == "" {
		var resp map[string]json.RawMessage
		if err := json.Unmarshal(body, &resp); err != nil {
			return errMsg("respuesta inesperada de %s", src), nil
		}
		var sb strings.Builder
		sb.WriteString("## Indicadores Económicos de Chile (hoy)\n\n")
		sb.WriteString(mdHeader("Indicador", "Valor", "Unidad", "Fecha"))
		for _, k := range indicadoresValidos {
			var m struct {
				Valor        float64 `json:"valor"`
				UnidadMedida string  `json:"unidad_medida"`
				Fecha        string  `json:"fecha"`
			}
			if raw, ok := resp[k]; !ok || json.Unmarshal(raw, &m) != nil {
				continue
			}
			sb.WriteString(mdRow(indicadorLabel[k]+" (`"+k+"`)", strconv.FormatFloat(m.Valor, 'f', -1, 64), m.UnidadMedida, truncateDate(m.Fecha)))
		}
		fmt.Fprintf(&sb, "\nFuente: %s\n", src)
		return textResult(sb.String()), nil
	}

	var resp struct {
		Nombre       string `json:"nombre"`
		UnidadMedida string `json:"unidad_medida"`
		Serie        []struct {
			Fecha string  `json:"fecha"`
			Valor float64 `json:"valor"`
		} `json:"serie"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errMsg("respuesta inesperada de %s", src), nil
	}
	if len(resp.Serie) == 0 {
		return textResult(fmt.Sprintf("Sin datos de %s para %s (fuente: %s).", indicador, orDefault(anio, "hoy"), src)), nil
	}
	sort.SliceStable(resp.Serie, func(i, j int) bool { return resp.Serie[i].Fecha > resp.Serie[j].Fecha })

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s (%s)\n", resp.Nombre, resp.UnidadMedida)
	n := min(len(resp.Serie), 30)
	fmt.Fprintf(&sb, "Registros: %d", len(resp.Serie))
	if n < len(resp.Serie) {
		fmt.Fprintf(&sb, " (mostrando los %d más recientes)", n)
	}
	sb.WriteString("\n\n" + mdHeader("Fecha", "Valor"))
	for _, s := range resp.Serie[:n] {
		sb.WriteString(mdRow(truncateDate(s.Fecha), strconv.FormatFloat(s.Valor, 'f', -1, 64)))
	}
	fmt.Fprintf(&sb, "\nFuente: %s\n", src)
	return textResult(sb.String()), nil
}

func truncateDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// ===================== Sismos (api.xor.cl / CSN, respaldo api.gael.cloud) =====================

type sismo struct {
	local, utc string
	lat, lon   *float64
	prof, mag  float64
	ref        string
}

func sismosXor(ctx context.Context) ([]sismo, error) {
	body, err := doGet(ctx, fuenteSismosXor, "https://api.xor.cl/sismo/recent")
	if err != nil {
		return nil, err
	}
	var r struct {
		Events []struct {
			LocalDate string  `json:"local_date"`
			UTCDate   string  `json:"utc_date"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Depth     float64 `json:"depth"`
			Magnitude struct {
				Value float64 `json:"value"`
			} `json:"magnitude"`
			GeoReference string `json:"geo_reference"`
		} `json:"events"`
	}
	if err := json.Unmarshal(body, &r); err != nil || len(r.Events) == 0 {
		return nil, fmt.Errorf("api.xor.cl respondió sin eventos")
	}
	out := make([]sismo, 0, len(r.Events))
	for _, e := range r.Events {
		lat, lon := e.Latitude, e.Longitude
		out = append(out, sismo{e.LocalDate, e.UTCDate, &lat, &lon, e.Depth, e.Magnitude.Value, e.GeoReference})
	}
	return out, nil
}

func sismosGael(ctx context.Context) ([]sismo, error) {
	body, err := doGet(ctx, fuenteSismosGael, "https://api.gael.cloud/general/public/sismos")
	if err != nil {
		return nil, err
	}
	var r []struct {
		Fecha         string `json:"Fecha"`
		Profundidad   string `json:"Profundidad"`
		Magnitud      string `json:"Magnitud"`
		RefGeografica string `json:"RefGeografica"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("respuesta inesperada de api.gael.cloud")
	}
	out := make([]sismo, 0, len(r))
	for _, e := range r {
		s := sismo{local: e.Fecha, ref: e.RefGeografica}
		s.prof, _ = strconv.ParseFloat(e.Profundidad, 64)
		s.mag, _ = strconv.ParseFloat(e.Magnitud, 64)
		// gael entrega hora de Chile; calculamos UTC.
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", e.Fecha, santiago); err == nil {
			s.utc = t.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, s)
	}
	return out, nil
}

func ultimosSismos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	magMin, hayMag := floatArg(req, "magnitud_minima")
	src := "Centro Sismológico Nacional vía api.xor.cl"
	sismos, err := sismosXor(ctx)
	if err != nil {
		var err2 error
		sismos, err2 = sismosGael(ctx)
		if err2 != nil {
			return errMsg("no se pudieron obtener sismos: %v; respaldo: %v", err, err2), nil
		}
		src = "api.gael.cloud (respaldo, sin coordenadas)"
	}

	var sb strings.Builder
	n := 0
	for _, s := range sismos {
		if hayMag && s.mag < magMin {
			continue
		}
		lat, lon := "", ""
		if s.lat != nil {
			lat, lon = strconv.FormatFloat(*s.lat, 'f', 3, 64), strconv.FormatFloat(*s.lon, 'f', 3, 64)
		}
		sb.WriteString(mdRow(s.local, s.utc, strconv.FormatFloat(s.mag, 'f', 1, 64), strconv.FormatFloat(s.prof, 'f', 0, 64), lat, lon, cell(s.ref, 80)))
		n++
	}
	head := fmt.Sprintf("## Últimos sismos en Chile — %d de %d registros", n, len(sismos))
	if hayMag {
		head += fmt.Sprintf(" (magnitud ≥ %.1f)", magMin)
	}
	head += "\nHora local de Chile (America/Santiago) y UTC. Fuente: " + src + "\n\n"
	if n == 0 {
		return textResult(head + "Ningún sismo reciente cumple el filtro.\n"), nil
	}
	return textResult(head + mdHeader("Hora Chile", "UTC", "Mag", "Prof km", "Lat", "Lon", "Referencia") + sb.String()), nil
}

// ===================== Calidad del Aire (SINCA) =====================

type estacionSINCA struct {
	Nombre   string `json:"nombre"`
	Comuna   string `json:"comuna"`
	Red      string `json:"red"`
	Region   string `json:"region"`
	Empresa  string `json:"empresa"`
	Realtime []struct {
		TableRow struct {
			Status     string   `json:"status"`
			StatusCode int      `json:"statuscode"`
			Datetime   string   `json:"datetime"`
			Parameter  string   `json:"parameter"`
			Movil      string   `json:"movil"`
			Value      float64  `json:"value"`
			Unit       string   `json:"unit"`
			Icap       *float64 `json:"icap"`
		} `json:"tableRow"`
	} `json:"realtime"`
}

func (e estacionSINCA) conDatos() bool {
	for _, rt := range e.Realtime {
		if rt.TableRow.Parameter != "" {
			return true
		}
	}
	return false
}

// peorEstado devuelve el estado con mayor statuscode de la estación (bueno < regular < alerta ...).
func (e estacionSINCA) peorEstado() (string, int) {
	st, code := "", 0
	for _, rt := range e.Realtime {
		if rt.TableRow.Parameter != "" && rt.TableRow.StatusCode >= code {
			st, code = rt.TableRow.Status, rt.TableRow.StatusCode
		}
	}
	return st, code
}

func calidadAire(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	regionIn := stringArg(req, "region")
	regionFilter := normText(regionIn)
	if r, ok := buscarRegion(regionIn); ok {
		regionFilter = normText(r.nombre) // "13" / "RM" / "XIII" -> "metropolitana"
	}
	comunaFilter := normText(stringArg(req, "comuna"))

	body, err := doGet(ctx, fuenteSINCA, "https://sinca.mma.gob.cl/index.php/json/listadomapa2k19/")
	if err != nil {
		return errResult(err), nil
	}
	var estaciones []estacionSINCA
	if err := json.Unmarshal(body, &estaciones); err != nil {
		return errMsg("respuesta inesperada de SINCA: %v", err), nil
	}

	var match []estacionSINCA
	for _, e := range estaciones {
		if regionFilter != "" && !strings.Contains(normText(e.Region), regionFilter) {
			continue
		}
		if comunaFilter != "" && !strings.Contains(normText(e.Comuna), comunaFilter) {
			continue
		}
		match = append(match, e)
	}

	if len(match) == 0 {
		return textResult(sincaSugerencias(estaciones, regionFilter, comunaFilter)), nil
	}
	if regionFilter == "" && comunaFilter == "" {
		return textResult(sincaResumen(match)), nil
	}

	var blocks []string
	for _, e := range match {
		var b strings.Builder
		fmt.Fprintf(&b, "## %s (%s, %s)\n", cleanText(e.Nombre), cleanText(e.Comuna), cleanText(e.Region))
		fmt.Fprintf(&b, "Red: %s | Empresa: %s\n", cleanText(e.Red), cleanText(e.Empresa))
		if !e.conDatos() {
			b.WriteString("- (sin datos en tiempo real)\n\n")
			blocks = append(blocks, b.String())
			continue
		}
		for _, rt := range e.Realtime {
			tr := rt.TableRow
			if tr.Parameter == "" {
				continue
			}
			icap := "-"
			if tr.Icap != nil {
				icap = strconv.FormatFloat(*tr.Icap, 'f', 0, 64)
			}
			fmt.Fprintf(&b, "- %s: **%s %s** (%s) | Estado: %s | ICAP: %s | medido: %s\n",
				cleanText(tr.Parameter), strconv.FormatFloat(tr.Value, 'f', -1, 64), cleanText(tr.Unit), cleanText(tr.Movil), orDefault(tr.Status, "-"), icap, tr.Datetime)
		}
		b.WriteString("\n")
		blocks = append(blocks, b.String())
	}
	text, n := fitBlocks(blocks, maxOutput-400)
	header := fmt.Sprintf("## Calidad del Aire — %d estaciones (hora de Chile)\n\n", len(match))
	if n < len(match) {
		header += fmt.Sprintf("Mostrando %d de %d; filtrá por comuna para ver el resto.\n\n", n, len(match))
	}
	return textResult(header + text), nil
}

// sincaResumen: sin filtros, una fila por región con estaciones y el peor estado.
func sincaResumen(es []estacionSINCA) string {
	type agg struct {
		region          string
		total, sinDatos int
		peor            string
		peorCode        int
		peorEst         string
	}
	m := map[string]*agg{}
	var orden []string
	for _, e := range es {
		reg := cleanText(e.Region)
		a, ok := m[reg]
		if !ok {
			a = &agg{region: reg}
			m[reg] = a
			orden = append(orden, reg)
		}
		a.total++
		if !e.conDatos() {
			a.sinDatos++
			continue
		}
		if st, code := e.peorEstado(); code > a.peorCode {
			a.peor, a.peorCode, a.peorEst = st, code, cleanText(e.Nombre)+" ("+cleanText(e.Comuna)+")"
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## Calidad del Aire — resumen de %d estaciones\n\n", len(es))
	sb.WriteString(mdHeader("Región", "Estaciones", "Sin datos", "Peor estado", "Dónde"))
	for _, r := range orden {
		a := m[r]
		sb.WriteString(mdRow(a.region, strconv.Itoa(a.total), strconv.Itoa(a.sinDatos), orDefault(a.peor, "-"), a.peorEst))
	}
	sb.WriteString("\nFiltrá con region (ej: 'Metropolitana' o '13') o comuna (ej: 'Temuco') para ver cada estación y contaminante.\n")
	return sb.String()
}

func sincaSugerencias(es []estacionSINCA, region, comuna string) string {
	regs := map[string]bool{}
	comunas := map[string]bool{}
	for _, e := range es {
		regs[cleanText(e.Region)] = true
		if region == "" || strings.Contains(normText(e.Region), region) {
			comunas[cleanText(e.Comuna)] = true
		}
	}
	keys := func(m map[string]bool) []string {
		out := make([]string, 0, len(m))
		for k := range m {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	var sb strings.Builder
	sb.WriteString("No hay estaciones de SINCA para ese filtro.\n\n")
	if comuna != "" && len(comunas) > 0 {
		sb.WriteString("Comunas con estación")
		if region != "" {
			sb.WriteString(" en esa región")
		}
		sb.WriteString(": " + strings.Join(keys(comunas), ", ") + "\n\n")
	}
	sb.WriteString("Regiones válidas: " + strings.Join(keys(regs), "; ") + "\n")
	return sb.String()
}

// ===================== Farmacias de turno (Farmanet MINSAL) =====================

type localFarmanet struct {
	Fecha     string `json:"fecha"`
	FkRegion  string `json:"fk_region"`
	Nombre    string `json:"local_nombre"`
	Comuna    string `json:"comuna_nombre"`
	Localidad string `json:"localidad_nombre"`
	Direccion string `json:"local_direccion"`
	Apertura  string `json:"funcionamiento_hora_apertura"`
	Cierre    string `json:"funcionamiento_hora_cierre"`
	Telefono  string `json:"local_telefono"`
	Dia       string `json:"funcionamiento_dia"`
}

// limpiarTelefono deja solo dígitos (y '+'); descarta valores basura como "+56" o "0".
func limpiarTelefono(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	d := b.String()
	if strings.HasPrefix(d, "56") && len(d) >= 10 {
		d = d[2:]
	}
	d = strings.TrimLeft(d, "0")
	if len(d) < 7 {
		return ""
	}
	if len(d) == 9 {
		return "+56 " + d
	}
	return d
}

func hhmm(s string) string {
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}

func farmaciasTurno(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	comuna := normText(stringArg(req, "comuna"))
	regionIn := stringArg(req, "region")
	limite := limitArg(req, "limite", 10, 100)

	var reg region
	if regionIn != "" {
		r, ok := buscarRegion(regionIn)
		if !ok {
			return errMsg("región %q no reconocida. Usá número oficial, romano o nombre: %s", regionIn, listaRegiones()), nil
		}
		reg = r
	}

	body, err := doGet(ctx, fuenteFarmanet, "https://midas.minsal.cl/farmacia_v2/WS/getLocalesTurnos.php")
	if err != nil {
		return errResult(err), nil
	}
	var locales []localFarmanet
	if err := json.Unmarshal(body, &locales); err != nil {
		return errMsg("respuesta inesperada de Farmanet: %v", err), nil
	}

	hoy := time.Now().In(santiago).Format("2006-01-02")
	var match []localFarmanet
	viejos := 0
	for _, f := range locales {
		if reg.minsal != "" && f.FkRegion != reg.minsal {
			continue
		}
		if comuna != "" && !strings.Contains(normText(f.Comuna), comuna) && !strings.Contains(normText(f.Localidad), comuna) {
			continue
		}
		if f.Fecha != hoy {
			viejos++
			continue
		}
		match = append(match, f)
	}

	donde := ""
	if reg.nombre != "" {
		donde = " en región " + reg.nombre
	}
	if comuna != "" {
		donde += " en comuna " + strconv.Quote(stringArg(req, "comuna"))
	}
	nota := ""
	if viejos > 0 {
		nota = fmt.Sprintf("\nSe descartaron %d locales con turno de otra fecha (datos desactualizados de MINSAL).\n", viejos)
	}
	if len(match) == 0 {
		return textResult(fmt.Sprintf("No se encontraron farmacias de turno para hoy (%s)%s (Farmanet/MINSAL).%s", hoy, donde, nota)), nil
	}

	var sb strings.Builder
	n := min(limite, len(match))
	fmt.Fprintf(&sb, "## Farmacias de turno hoy %s%s — mostrando %d de %d\n\n", hoy, donde, n, len(match))
	sb.WriteString(mdHeader("Local", "Comuna", "Dirección", "Teléfono", "Horario"))
	for _, f := range match[:n] {
		sb.WriteString(mdRow(cell(f.Nombre, 40), cell(f.Comuna, 30), cell(f.Direccion, 60),
			orDefault(limpiarTelefono(f.Telefono), "sin teléfono"), hhmm(f.Apertura)+"–"+hhmm(f.Cierre)))
	}
	if n < len(match) {
		fmt.Fprintf(&sb, "\nHay %d más; subí limite (máx 100) o filtrá por comuna.\n", len(match)-n)
	}
	sb.WriteString(nota)
	return textResult(sb.String()), nil
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
