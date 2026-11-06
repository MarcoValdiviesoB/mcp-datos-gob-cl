package sinca

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(httpx.New("test"))
	c.URL = srv.URL
	c.Fuente = httpx.Fuente{Nombre: "SINCA (MMA)", Timeout: time.Second, Intentos: 1}
	return c
}

// listadomapa.json: respuesta real de SINCA (2026-11), algunas estaciones y sin las series horarias.
func TestEstaciones(t *testing.T) {
	b, err := os.ReadFile("testdata/listadomapa.json")
	if err != nil {
		t.Fatal(err)
	}
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) })
	es, err := c.Estaciones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	conDatos, sinDatos := 0, 0
	for _, e := range es {
		if e.ConDatos() {
			conDatos++
			if st, code := e.PeorEstado(); st == "" || code == 0 {
				t.Errorf("%s: PeorEstado vacío", e.Nombre)
			}
		} else {
			sinDatos++
		}
	}
	if len(es) < 5 || conDatos == 0 || sinDatos == 0 {
		t.Errorf("estaciones=%d conDatos=%d sinDatos=%d", len(es), conDatos, sinDatos)
	}
}

func TestEstacionesError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) })
	if _, err := c.Estaciones(context.Background()); err == nil || !strings.Contains(err.Error(), "respuesta inesperada de SINCA") {
		t.Errorf("err = %v", err)
	}
	c = testClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	if _, err := c.Estaciones(context.Background()); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("err = %v", err)
	}
}
