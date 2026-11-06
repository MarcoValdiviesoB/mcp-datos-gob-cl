package main

import (
	"encoding/json"
	"fmt"
	"html"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// maxOutput es el tope de texto que devuelve una tool (~20 KB) para no inundar el contexto del LLM.
const maxOutput = 20_000

// ===================== Argumentos =====================

// stringArg lee un argumento como texto. Acepta números (ej. anio: 2025) además de strings.
func stringArg(req mcp.CallToolRequest, name string) string {
	v, ok := req.GetArguments()[name]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case bool:
		return strconv.FormatBool(t)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// intArg lee un argumento entero. Acepta número o string ("10", " 10 ", "10.0"); si no se puede leer usa def.
func intArg(req mcp.CallToolRequest, name string, def int) int {
	s := stringArg(req, name)
	if s == "" {
		return def
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return def
	}
	if f > math.MaxInt32 {
		return math.MaxInt32
	}
	if f < math.MinInt32 {
		return math.MinInt32
	}
	return int(f)
}

// floatArg lee un argumento decimal (número o string). ok=false si no vino o no es número.
func floatArg(req mcp.CallToolRequest, name string) (float64, bool) {
	s := strings.ReplaceAll(stringArg(req, name), ",", ".")
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// clampInt deja v dentro de [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// limitArg lee un "limite": 0 o negativo usa el default; lo demás se acota a [1, max].
func limitArg(req mcp.CallToolRequest, name string, def, max int) int {
	v := intArg(req, name, def)
	if v <= 0 {
		v = def
	}
	return clampInt(v, 1, max)
}

// offsetArg lee un offset/página acotado a [lo, max].
func offsetArg(req mcp.CallToolRequest, name string, def, lo, max int) int {
	return clampInt(intArg(req, name, def), lo, max)
}

// ===================== Texto =====================

// truncate corta s a n runas (no bytes, para no romper UTF-8) y agrega "…" si cortó.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n])) + "…"
}

var tagRe = regexp.MustCompile(`<[^>]+>`)
var spaceRe = regexp.MustCompile(`\s+`)

// cleanText decodifica entidades HTML, quita tags (ej. unidades de SINCA) y colapsa espacios.
func cleanText(s string) string {
	s = tagRe.ReplaceAllString(html.UnescapeString(s), "")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

var diacritics = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
)

// normText normaliza para comparar: minúsculas, sin tildes ni ñ, sin apóstrofes, espacios colapsados.
func normText(s string) string {
	s = diacritics.Replace(strings.ToLower(s))
	s = strings.NewReplacer("'", "", "’", "", ".", " ").Replace(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// cell prepara un valor para una celda de tabla markdown: una línea, sin '|', acotado.
func cell(v any, n int) string {
	if v == nil {
		return ""
	}
	s := cleanText(fmt.Sprint(v))
	s = strings.ReplaceAll(s, "|", "/")
	return truncate(s, n)
}

// mdRow arma una fila de tabla markdown.
func mdRow(cells ...string) string {
	return "| " + strings.Join(cells, " | ") + " |\n"
}

// mdHeader arma encabezado + separador de una tabla markdown.
func mdHeader(cols ...string) string {
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	return mdRow(cols...) + mdRow(sep...)
}

// fitBlocks concatena bloques (filas o registros) mientras quepan en budget bytes. Devuelve el texto y cuántos entraron.
func fitBlocks(blocks []string, budget int) (string, int) {
	var sb strings.Builder
	for i, b := range blocks {
		if sb.Len()+len(b) > budget && i > 0 {
			return sb.String(), i
		}
		sb.WriteString(b)
	}
	return sb.String(), len(blocks)
}

// capOutput es la red de seguridad final: recorta en un salto de línea si el texto pasa maxOutput.
func capOutput(s string) string {
	if len(s) <= maxOutput {
		return s
	}
	cut := s[:maxOutput]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, '\n'); i > maxOutput/2 {
		cut = cut[:i+1]
	}
	return cut + fmt.Sprintf("\n…[salida recortada: se muestran %d de %d caracteres. Usá limite/offset/pagina o filtros para ver el resto]\n", len(cut), len(s))
}

func textResult(s string) *mcp.CallToolResult {
	return mcp.NewToolResultText(capOutput(s))
}

func errResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(redact(err.Error()))
}

func errMsg(format string, a ...any) *mcp.CallToolResult {
	return mcp.NewToolResultError(redact(fmt.Sprintf(format, a...)))
}

var ticketRe = regexp.MustCompile(`(?i)(ticket=)[^&\s"']+`)

// redact borra cualquier ticket que se haya colado en un mensaje (defensa en profundidad).
func redact(s string) string {
	return ticketRe.ReplaceAllString(s, "${1}***")
}

// ===================== RUT =====================

// normRUT deja un RUT como "12345678-K": sin puntos ni espacios, con guion y DV en mayúscula.
// Valida el dígito verificador.
func normRUT(s string) (string, error) {
	s = strings.ToUpper(strings.NewReplacer(".", "", " ", "", "-", "", "‐", "").Replace(strings.TrimSpace(s)))
	if len(s) < 2 {
		return "", fmt.Errorf("RUT vacío o muy corto; usá el formato 12.345.678-9")
	}
	body, dv := s[:len(s)-1], s[len(s)-1:]
	body = strings.TrimLeft(body, "0")
	if body == "" || len(body) > 9 {
		return "", fmt.Errorf("RUT %q inválido; usá el formato 12.345.678-9", s)
	}
	for _, c := range body {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("RUT %q inválido: el cuerpo debe ser numérico (formato 12.345.678-9)", s)
		}
	}
	if want := rutDV(body); want != dv {
		return "", fmt.Errorf("RUT %s-%s con dígito verificador incorrecto (para %s debería ser %s)", body, dv, body, want)
	}
	return body + "-" + dv, nil
}

// rutDV calcula el dígito verificador (módulo 11) de un cuerpo numérico.
func rutDV(body string) string {
	sum, mul := 0, 2
	for i := len(body) - 1; i >= 0; i-- {
		sum += int(body[i]-'0') * mul
		mul++
		if mul > 7 {
			mul = 2
		}
	}
	switch r := 11 - sum%11; r {
	case 11:
		return "0"
	case 10:
		return "K"
	default:
		return strconv.Itoa(r)
	}
}

// rutConPuntos formatea un RUT normalizado como lo espera Mercado Público: "70.017.820-k".
func rutConPuntos(rut string) string {
	body, dv, _ := strings.Cut(rut, "-")
	var out []byte
	for i := range body {
		if i > 0 && (len(body)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, body[i])
	}
	return string(out) + "-" + strings.ToLower(dv)
}

// ===================== Regiones =====================

type region struct {
	num    int      // número oficial (1-16)
	nombre string   // nombre corto
	alias  []string // nombres alternativos y romanos, ya normalizados
	minsal string   // fk_region de Farmanet (MINSAL), ordenado de norte a sur
}

// regiones mapea el número oficial a fk_region de MINSAL. Construido contra getLocalesTurnos (2026-11):
// MINSAL numera de norte a sur (1=Arica ... 16=Ñuble), no con el número oficial.
var regiones = []region{
	{15, "Arica y Parinacota", []string{"xv", "arica"}, "1"},
	{1, "Tarapacá", []string{"i", "tarapaca"}, "2"},
	{2, "Antofagasta", []string{"ii"}, "3"},
	{3, "Atacama", []string{"iii"}, "4"},
	{4, "Coquimbo", []string{"iv"}, "5"},
	{5, "Valparaíso", []string{"v"}, "6"},
	{13, "Metropolitana", []string{"xiii", "rm", "santiago", "metropolitana de santiago", "region metropolitana"}, "7"},
	{6, "O'Higgins", []string{"vi", "libertador", "libertador general bernardo ohiggins"}, "8"},
	{7, "Maule", []string{"vii"}, "9"},
	{8, "Biobío", []string{"viii", "bio bio", "biobio"}, "10"},
	{9, "La Araucanía", []string{"ix", "araucania"}, "11"},
	{14, "Los Ríos", []string{"xiv", "rios"}, "12"},
	{10, "Los Lagos", []string{"x", "lagos"}, "13"},
	{11, "Aysén", []string{"xi", "aysen", "aisen"}, "14"},
	{12, "Magallanes", []string{"xii", "magallanes y antartica chilena"}, "15"},
	{16, "Ñuble", []string{"xvi", "nuble"}, "16"},
}

// buscarRegion interpreta número oficial ("13"), romano ("XIII"), "RM" o nombre ("Valparaíso", "region de los rios").
func buscarRegion(s string) (region, bool) {
	n := normText(s)
	n = strings.TrimPrefix(n, "region ")
	n = strings.TrimPrefix(n, "de ")
	n = strings.TrimPrefix(n, "del ")
	if n == "" {
		return region{}, false
	}
	if num, err := strconv.Atoi(n); err == nil {
		for _, r := range regiones {
			if r.num == num {
				return r, true
			}
		}
		return region{}, false
	}
	for _, r := range regiones {
		if normText(r.nombre) == n {
			return r, true
		}
		for _, a := range r.alias {
			if a == n {
				return r, true
			}
		}
	}
	for _, r := range regiones { // coincidencia parcial: "valpo" no, pero "araucan" sí
		if len(n) >= 4 && strings.Contains(normText(r.nombre), n) {
			return r, true
		}
	}
	return region{}, false
}

func listaRegiones() string {
	rs := append([]region(nil), regiones...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].num < rs[j].num })
	var parts []string
	for _, r := range rs {
		parts = append(parts, fmt.Sprintf("%d=%s", r.num, r.nombre))
	}
	return strings.Join(parts, ", ")
}

// ===================== JSON =====================

// stringifyFilters convierte los valores de un JSON de filtros a string (DataStore compara texto exacto;
// un número contra un campo text da "Invalid query"). Acepta listas de valores.
func stringifyFilters(raw string) (string, error) {
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
