package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== Sismos (api.xor.cl / CSN, respaldo api.gael.cloud) =====================

func (h *handlers) ultimosSismos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	magMin, hayMag := floatArg(req, "magnitud_minima")
	sismos, src, err := h.Sismos.Recientes(ctx)
	if err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	n := 0
	for _, s := range sismos {
		if hayMag && s.Mag < magMin {
			continue
		}
		lat, lon := "", ""
		if s.Lat != nil {
			lat, lon = strconv.FormatFloat(*s.Lat, 'f', 3, 64), strconv.FormatFloat(*s.Lon, 'f', 3, 64)
		}
		sb.WriteString(text.MDRow(s.Local, s.UTC, strconv.FormatFloat(s.Mag, 'f', 1, 64), strconv.FormatFloat(s.Prof, 'f', 0, 64), lat, lon, text.Cell(s.Referencia, 80)))
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
	return textResult(head + text.MDHeader("Hora Chile", "UTC", "Mag", "Prof km", "Lat", "Lon", "Referencia") + sb.String()), nil
}
