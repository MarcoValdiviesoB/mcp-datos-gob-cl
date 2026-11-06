package tools

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func req(args map[string]any) mcp.CallToolRequest {
	var r mcp.CallToolRequest
	r.Params.Arguments = args
	return r
}

func TestClampAndArgs(t *testing.T) {
	if clampInt(-5, 0, 10) != 0 || clampInt(50, 0, 10) != 10 || clampInt(5, 0, 10) != 5 {
		t.Error("clampInt")
	}
	r := req(map[string]any{
		"num": float64(25), "str": " 30 ", "dec": "12.0", "neg": float64(-3), "zero": float64(0),
		"huge": float64(1e15), "basura": "abc", "anio": float64(2025),
	})
	cases := []struct {
		name     string
		got, exp int
	}{
		{"num", limitArg(r, "num", 10, 100), 25},
		{"str", limitArg(r, "str", 10, 100), 30},
		{"dec", limitArg(r, "dec", 10, 100), 12},
		{"neg->default", limitArg(r, "neg", 10, 100), 10},
		{"zero->default", limitArg(r, "zero", 10, 100), 10},
		{"huge->max", limitArg(r, "huge", 10, 100), 100},
		{"basura->default", limitArg(r, "basura", 10, 100), 10},
		{"ausente->default", limitArg(r, "nada", 10, 100), 10},
		{"offset neg->0", offsetArg(r, "neg", 0, 0, 1000), 0},
		{"offset huge->max", offsetArg(r, "huge", 0, 0, 1000), 1000},
		{"muestra 0 permitido", offsetArg(r, "zero", 3, 0, 50), 0},
	}
	for _, c := range cases {
		if c.got != c.exp {
			t.Errorf("%s: got %d want %d", c.name, c.got, c.exp)
		}
	}
	if got := stringArg(r, "anio"); got != "2025" {
		t.Errorf("anio numérico debe leerse como string: %q", got)
	}
	if got := stringArg(r, "str"); got != "30" {
		t.Errorf("stringArg debe hacer TrimSpace: %q", got)
	}
}

func TestFloatArg(t *testing.T) {
	r := req(map[string]any{"a": "4,5", "b": float64(3), "c": "x", "d": true, "e": 7})
	if f, ok := floatArg(r, "a"); !ok || f != 4.5 {
		t.Errorf("coma decimal: %v %v", f, ok)
	}
	if f, ok := floatArg(r, "b"); !ok || f != 3 {
		t.Errorf("número: %v %v", f, ok)
	}
	if _, ok := floatArg(r, "c"); ok {
		t.Error("basura no es número")
	}
	if _, ok := floatArg(r, "nada"); ok {
		t.Error("ausente")
	}
	if stringArg(r, "d") != "true" || stringArg(r, "e") != "7" {
		t.Error("stringArg de bool/int")
	}
	if intArg(req(map[string]any{"x": float64(-1e15)}), "x", 0) >= 0 {
		t.Error("intArg debe acotar negativos enormes")
	}
}

func TestFormatoMP(t *testing.T) {
	if got := pesos(1234567, "CLP"); got != "$1.234.567 CLP" {
		t.Errorf("pesos = %q", got)
	}
	if got := fechaMP("2026-11-05T15:30:00.123"); got != "2026-11-05 15:30" {
		t.Errorf("fechaMP = %q", got)
	}
	if got := truncateDate("2026-11-05T03:00:00.000Z"); got != "2026-11-05" {
		t.Errorf("truncateDate = %q", got)
	}
	if !strings.HasPrefix(truncateDate("hoy"), "hoy") {
		t.Error("truncateDate corto")
	}
}
