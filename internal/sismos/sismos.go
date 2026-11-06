// Package sismos trae los últimos sismos del Centro Sismológico Nacional vía api.xor.cl,
// con respaldo en api.gael.cloud.
package sismos

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

var (
	FuenteXor  = httpx.Fuente{Nombre: "api.xor.cl (CSN)", Timeout: 8 * time.Second, Intentos: 2, Espera: 500 * time.Millisecond}
	FuenteGael = httpx.Fuente{Nombre: "api.gael.cloud", Timeout: 10 * time.Second, Intentos: 2, Espera: 500 * time.Millisecond}
)

// Sismo es un evento. Lat/Lon son nil cuando la fuente no trae coordenadas (gael).
type Sismo struct {
	Local      string   `json:"hora_chile"`
	UTC        string   `json:"utc"`
	Lat        *float64 `json:"latitud,omitempty"`
	Lon        *float64 `json:"longitud,omitempty"`
	Prof       float64  `json:"profundidad_km"`
	Mag        float64  `json:"magnitud"`
	Referencia string   `json:"referencia"`
}

// Client consulta las dos fuentes.
type Client struct {
	HTTP       *httpx.Client
	XorURL     string
	GaelURL    string
	FuenteXor  httpx.Fuente
	FuenteGael httpx.Fuente
}

// New crea un cliente contra api.xor.cl con respaldo en api.gael.cloud.
func New(hc *httpx.Client) *Client {
	return &Client{HTTP: hc, XorURL: "https://api.xor.cl/sismo/recent", GaelURL: "https://api.gael.cloud/general/public/sismos",
		FuenteXor: FuenteXor, FuenteGael: FuenteGael}
}

// Recientes trae los últimos sismos y el nombre de la fuente usada.
func (c *Client) Recientes(ctx context.Context) ([]Sismo, string, error) {
	sismos, err := c.xor(ctx)
	if err == nil {
		return sismos, "Centro Sismológico Nacional vía api.xor.cl", nil
	}
	sismos, err2 := c.gael(ctx)
	if err2 != nil {
		return nil, "", fmt.Errorf("no se pudieron obtener sismos: %v; respaldo: %v", err, err2)
	}
	return sismos, "api.gael.cloud (respaldo, sin coordenadas)", nil
}

func (c *Client) xor(ctx context.Context) ([]Sismo, error) {
	body, err := c.HTTP.Get(ctx, c.FuenteXor, c.XorURL)
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
	out := make([]Sismo, 0, len(r.Events))
	for _, e := range r.Events {
		lat, lon := e.Latitude, e.Longitude
		out = append(out, Sismo{e.LocalDate, e.UTCDate, &lat, &lon, e.Depth, e.Magnitude.Value, e.GeoReference})
	}
	return out, nil
}

func (c *Client) gael(ctx context.Context) ([]Sismo, error) {
	body, err := c.HTTP.Get(ctx, c.FuenteGael, c.GaelURL)
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
	out := make([]Sismo, 0, len(r))
	for _, e := range r {
		s := Sismo{Local: e.Fecha, Referencia: e.RefGeografica}
		s.Prof, _ = strconv.ParseFloat(e.Profundidad, 64)
		s.Mag, _ = strconv.ParseFloat(e.Magnitud, 64)
		// gael entrega hora de Chile; calculamos UTC.
		if t, err := time.ParseInLocation("2006-01-02 15:04:05", e.Fecha, text.Santiago); err == nil {
			s.UTC = t.UTC().Format("2006-01-02 15:04:05")
		}
		out = append(out, s)
	}
	return out, nil
}
