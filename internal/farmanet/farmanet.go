// Package farmanet trae las farmacias de turno de Farmanet (MINSAL).
package farmanet

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
)

// FuenteFarmanet: timeout y reintentos para Farmanet.
var FuenteFarmanet = httpx.Fuente{Nombre: "Farmanet (MINSAL)", Timeout: 15 * time.Second, Intentos: 3, Espera: 500 * time.Millisecond}

// Local es una farmacia de turno.
type Local struct {
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

// Client consulta Farmanet.
type Client struct {
	HTTP   *httpx.Client
	URL    string
	Fuente httpx.Fuente
}

// New crea un cliente contra midas.minsal.cl.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, URL: "https://midas.minsal.cl/farmacia_v2/WS/getLocalesTurnos.php", Fuente: FuenteFarmanet}
}

// Locales trae todos los locales de turno publicados (pueden venir fechas viejas).
func (c *Client) Locales(ctx context.Context) ([]Local, error) {
	body, err := c.HTTP.Get(ctx, c.Fuente, c.URL)
	if err != nil {
		return nil, err
	}
	var locales []Local
	if err := json.Unmarshal(body, &locales); err != nil {
		return nil, fmt.Errorf("respuesta inesperada de Farmanet: %v", err)
	}
	return locales, nil
}

// LimpiarTelefono deja solo dígitos (y '+'); descarta valores basura como "+56" o "0".
func LimpiarTelefono(s string) string {
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

// HHMM recorta "09:00:00" a "09:00".
func HHMM(s string) string {
	if len(s) >= 5 {
		return s[:5]
	}
	return s
}
