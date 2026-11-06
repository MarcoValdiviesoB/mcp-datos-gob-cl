// Package text agrupa la normalización y el formato de texto compartidos: truncado por runas,
// limpieza de HTML, tablas markdown, recorte de salida, RUT y regiones.
package text

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MaxOutput es el tope de texto que devuelve una tool (~20 KB) para no inundar el contexto del LLM.
const MaxOutput = 20_000

// Truncate corta s a n runas (no bytes, para no romper UTF-8) y agrega "…" si cortó.
func Truncate(s string, n int) string {
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

// CleanText decodifica entidades HTML, quita tags (ej. unidades de SINCA) y colapsa espacios.
func CleanText(s string) string {
	s = tagRe.ReplaceAllString(html.UnescapeString(s), "")
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

var diacritics = strings.NewReplacer(
	"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	"à", "a", "è", "e", "ì", "i", "ò", "o", "ù", "u",
)

// NormText normaliza para comparar: minúsculas, sin tildes ni ñ, sin apóstrofes, espacios colapsados.
func NormText(s string) string {
	s = diacritics.Replace(strings.ToLower(s))
	s = strings.NewReplacer("'", "", "’", "", ".", " ").Replace(s)
	return strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
}

// Cell prepara un valor para una celda de tabla markdown: una línea, sin '|', acotado.
func Cell(v any, n int) string {
	if v == nil {
		return ""
	}
	s := CleanText(fmt.Sprint(v))
	s = strings.ReplaceAll(s, "|", "/")
	return Truncate(s, n)
}

// MDRow arma una fila de tabla markdown.
func MDRow(cells ...string) string {
	return "| " + strings.Join(cells, " | ") + " |\n"
}

// MDHeader arma encabezado + separador de una tabla markdown.
func MDHeader(cols ...string) string {
	sep := make([]string, len(cols))
	for i := range sep {
		sep[i] = "---"
	}
	return MDRow(cols...) + MDRow(sep...)
}

// FitBlocks concatena bloques (filas o registros) mientras quepan en budget bytes. Devuelve el texto y cuántos entraron.
func FitBlocks(blocks []string, budget int) (string, int) {
	var sb strings.Builder
	for i, b := range blocks {
		if sb.Len()+len(b) > budget && i > 0 {
			return sb.String(), i
		}
		sb.WriteString(b)
	}
	return sb.String(), len(blocks)
}

// CapOutput es la red de seguridad final: recorta en un salto de línea si el texto pasa MaxOutput.
func CapOutput(s string) string {
	if len(s) <= MaxOutput {
		return s
	}
	cut := s[:MaxOutput]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndexByte(cut, '\n'); i > MaxOutput/2 {
		cut = cut[:i+1]
	}
	return cut + fmt.Sprintf("\n…[salida recortada: se muestran %d de %d caracteres. Usá limite/offset/pagina o filtros para ver el resto]\n", len(cut), len(s))
}

var ticketRe = regexp.MustCompile(`(?i)(ticket=)[^&\s"']+`)

// Redact borra cualquier ticket que se haya colado en un mensaje (defensa en profundidad).
func Redact(s string) string {
	return ticketRe.ReplaceAllString(s, "${1}***")
}

// OrDefault devuelve def si s está vacío o solo tiene espacios.
func OrDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
