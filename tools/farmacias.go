package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/farmanet"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== Farmacias de turno (Farmanet MINSAL) =====================

func (h *handlers) farmaciasTurno(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	comuna := text.NormText(stringArg(req, "comuna"))
	regionIn := stringArg(req, "region")
	limite := limitArg(req, "limite", 10, 100)

	var reg text.Region
	if regionIn != "" {
		r, ok := text.BuscarRegion(regionIn)
		if !ok {
			return errMsg("región %q no reconocida. Usá número oficial, romano o nombre: %s", regionIn, text.ListaRegiones()), nil
		}
		reg = r
	}

	locales, err := h.Farmanet.Locales(ctx)
	if err != nil {
		return errResult(err), nil
	}

	hoy := h.Now().In(text.Santiago).Format("2006-01-02")
	var match []farmanet.Local
	viejos := 0
	for _, f := range locales {
		if reg.Minsal != "" && f.FkRegion != reg.Minsal {
			continue
		}
		if comuna != "" && !strings.Contains(text.NormText(f.Comuna), comuna) && !strings.Contains(text.NormText(f.Localidad), comuna) {
			continue
		}
		if f.Fecha != hoy {
			viejos++
			continue
		}
		match = append(match, f)
	}

	donde := ""
	if reg.Nombre != "" {
		donde = " en región " + reg.Nombre
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
	sb.WriteString(text.MDHeader("Local", "Comuna", "Dirección", "Teléfono", "Horario"))
	for _, f := range match[:n] {
		sb.WriteString(text.MDRow(text.Cell(f.Nombre, 40), text.Cell(f.Comuna, 30), text.Cell(f.Direccion, 60),
			text.OrDefault(farmanet.LimpiarTelefono(f.Telefono), "sin teléfono"), farmanet.HHMM(f.Apertura)+"–"+farmanet.HHMM(f.Cierre)))
	}
	if n < len(match) {
		fmt.Fprintf(&sb, "\nHay %d más; subí limite (máx 100) o filtrá por comuna.\n", len(match)-n)
	}
	sb.WriteString(nota)
	return textResult(sb.String()), nil
}
