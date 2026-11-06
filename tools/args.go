package tools

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

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

// ===================== Resultados =====================

func textResult(s string) *mcp.CallToolResult {
	return mcp.NewToolResultText(text.CapOutput(s))
}

// structResult devuelve el texto (recortado como siempre) más la salida estructurada.
func structResult(v any, s string) *mcp.CallToolResult {
	return mcp.NewToolResultStructured(v, text.CapOutput(s))
}

func errResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(text.Redact(err.Error()))
}

func errMsg(format string, a ...any) *mcp.CallToolResult {
	return mcp.NewToolResultError(text.Redact(fmt.Sprintf(format, a...)))
}
