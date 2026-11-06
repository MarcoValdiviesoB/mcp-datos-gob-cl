package text

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNormRUT(t *testing.T) {
	ok := map[string]string{
		"78325537-5":   "78325537-5",
		"78.325.537-5": "78325537-5",
		" 783255375 ":  "78325537-5",
		"76.286.049-k": "76286049-K",
		"76286049K":    "76286049-K",
		"70.017.820-k": "70017820-K",
		"070017820-K":  "70017820-K",
		"11.111.111-1": "11111111-1",
	}
	for in, want := range ok {
		got, err := NormRUT(in)
		if err != nil || got != want {
			t.Errorf("NormRUT(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "1", "78325537-4", "abc-5", "12.345.678-X", "1234567890-1"} {
		if got, err := NormRUT(bad); err == nil {
			t.Errorf("NormRUT(%q) = %q; want error", bad, got)
		}
	}
	if _, err := NormRUT("78325537-4"); err == nil || !strings.Contains(err.Error(), "debería ser 5") {
		t.Errorf("DV incorrecto debería sugerir el correcto: %v", err)
	}
	if RutDV("1") != "9" || RutDV("6") != "K" || RutDV("70017820") != "K" {
		t.Error("RutDV")
	}
}

func TestRutConPuntos(t *testing.T) {
	cases := map[string]string{"70017820-K": "70.017.820-k", "7000000-1": "7.000.000-1", "123-4": "123-4", "1234-5": "1.234-5"}
	for in, want := range cases {
		if got := RutConPuntos(in); got != want {
			t.Errorf("RutConPuntos(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	s := "Dirección de Presupuestos — ñandú"
	for n := 0; n < utf8.RuneCountInString(s)+2; n++ {
		got := Truncate(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("Truncate(%d) dejó UTF-8 inválido: %q", n, got)
		}
		if utf8.RuneCountInString(strings.TrimSuffix(got, "…")) > n {
			t.Errorf("Truncate(%d) = %q; demasiado largo", n, got)
		}
	}
	if Truncate("hola", 10) != "hola" {
		t.Error("no debe tocar textos cortos")
	}
	if Truncate("áéíóú", 3) != "áéí…" {
		t.Errorf("Truncate por runas: %q", Truncate("áéíóú", 3))
	}
}

func TestNormText(t *testing.T) {
	cases := map[string]string{
		"  Valparaíso ": "valparaiso",
		"ÑUÑOA":         "nunoa",
		"Región del Libertador General Bernardo O'Higgins\n": "region del libertador general bernardo ohiggins",
		"Concepción":    "concepcion",
		"Los   Ángeles": "los angeles",
	}
	for in, want := range cases {
		if got := NormText(in); got != want {
			t.Errorf("NormText(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestCleanTextAndTables(t *testing.T) {
	if got := CleanText("µg/m<sup>3</sup> &amp;  N\n"); got != "µg/m3 & N" {
		t.Errorf("CleanText = %q", got)
	}
	if got := Cell("a | b", 10); got != "a / b" {
		t.Errorf("Cell = %q", got)
	}
	if Cell(nil, 5) != "" {
		t.Error("Cell(nil)")
	}
	if got := MDHeader("A", "B"); got != "| A | B |\n| --- | --- |\n" {
		t.Errorf("MDHeader = %q", got)
	}
	if OrDefault("  ", "x") != "x" || OrDefault("y", "x") != "y" {
		t.Error("OrDefault")
	}
}

func TestBuscarRegion(t *testing.T) {
	cases := map[string]string{ // entrada -> fk_region de MINSAL
		"13": "7", "RM": "7", "xiii": "7", "Metropolitana": "7", "Región Metropolitana de Santiago": "7",
		"5": "6", "Valparaíso": "6", "6": "8", "O'Higgins": "8", "10": "13", "Los Lagos": "13",
		"15": "1", "arica": "1", "16": "16", "Ñuble": "16", "8": "10", "Biobío": "10", "bio bio": "10",
		"14": "12", "Los Ríos": "12", "9": "11", "Araucanía": "11", "11": "14", "Aysén": "14", "12": "15",
	}
	for in, want := range cases {
		r, ok := BuscarRegion(in)
		if !ok || r.Minsal != want {
			t.Errorf("BuscarRegion(%q) = %+v, %v; want minsal %s", in, r, ok, want)
		}
	}
	for _, bad := range []string{"", "17", "0", "narnia"} {
		if r, ok := BuscarRegion(bad); ok {
			t.Errorf("BuscarRegion(%q) = %+v; want no encontrada", bad, r)
		}
	}
	seen := map[string]bool{}
	for _, r := range Regiones {
		if seen[r.Minsal] {
			t.Errorf("fk_region %s repetido", r.Minsal)
		}
		seen[r.Minsal] = true
	}
	if len(Regiones) != 16 {
		t.Errorf("deben ser 16 regiones, hay %d", len(Regiones))
	}
	if l := ListaRegiones(); !strings.HasPrefix(l, "1=Tarapacá, 2=Antofagasta") || !strings.HasSuffix(l, "16=Ñuble") {
		t.Errorf("ListaRegiones = %q", l)
	}
}

func TestRedact(t *testing.T) {
	s := Redact(`Get "https://api.mercadopublico.cl/x?estado=activas&ticket=F8537A18-6766&fecha=1": timeout`)
	if strings.Contains(s, "F8537A18") || !strings.Contains(s, "ticket=***") {
		t.Errorf("ticket no redactado: %s", s)
	}
}

func TestOutputCaps(t *testing.T) {
	big := strings.Repeat("fila con ñandú y más texto\n", 2000)
	out := CapOutput(big)
	if len(out) > MaxOutput+300 || !utf8.ValidString(out) || !strings.Contains(out, "salida recortada") {
		t.Errorf("CapOutput: len=%d", len(out))
	}
	if CapOutput("corto") != "corto" {
		t.Error("CapOutput no debe tocar textos cortos")
	}
	text, n := FitBlocks([]string{"aaaa", "bbbb", "cccc"}, 9)
	if n != 2 || text != "aaaabbbb" {
		t.Errorf("FitBlocks = %q, %d", text, n)
	}
	if _, n := FitBlocks([]string{strings.Repeat("x", 50)}, 10); n != 1 {
		t.Error("FitBlocks siempre incluye al menos un bloque")
	}
}
