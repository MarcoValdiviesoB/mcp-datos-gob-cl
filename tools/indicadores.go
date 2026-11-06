package tools

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mindicador"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/text"
)

// ===================== Indicadores Económicos (mindicador.cl, respaldo findic.cl) =====================

// IndicadoresOut es la salida estructurada de indicadores_economicos.
type IndicadoresOut struct {
	Fuente    string           `json:"fuente"`
	Indicador string           `json:"indicador,omitempty" jsonschema:"Código del indicador; ausente si son los valores del día de todos"`
	Anio      string           `json:"anio,omitempty"`
	Nombre    string           `json:"nombre,omitempty"`
	Unidad    string           `json:"unidad,omitempty"`
	Total     int              `json:"total" jsonschema:"Cantidad de registros de la serie (o de indicadores del día)"`
	Valores   []IndicadorValor `json:"valores" jsonschema:"Valores del día o los más recientes de la serie (máx 30), de más nuevo a más viejo"`
}

// IndicadorValor es un valor puntual.
type IndicadorValor struct {
	Codigo string  `json:"codigo,omitempty"`
	Nombre string  `json:"nombre,omitempty"`
	Valor  float64 `json:"valor"`
	Unidad string  `json:"unidad,omitempty"`
	Fecha  string  `json:"fecha" jsonschema:"aaaa-mm-dd"`
}

func (h *handlers) indicadoresEconomicos(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	indicador := strings.ToLower(stringArg(req, "indicador"))
	anio := stringArg(req, "anio")

	if indicador != "" && !slices.Contains(mindicador.Validos, indicador) {
		return errMsg("indicador %q no válido. Usá uno de: %s", indicador, strings.Join(mindicador.Validos, ", ")), nil
	}
	if anio != "" {
		y, err := strconv.Atoi(anio)
		if !anioFmt.MatchString(anio) || err != nil || y < 1928 || y > h.Now().In(text.Santiago).Year() {
			return errMsg("anio debe ser un año de 4 dígitos entre 1928 y el actual, ej: 2025"), nil
		}
		if indicador == "" {
			return errMsg("para una serie histórica indicá también 'indicador' (ej: indicador=uf, anio=%s)", anio), nil
		}
	}

	if indicador == "" {
		valores, src, err := h.Indicadores.Hoy(ctx)
		if errors.Is(err, mindicador.ErrDecode) {
			return errMsg("respuesta inesperada de %s", src), nil
		}
		if err != nil {
			return errResult(err), nil
		}
		st := IndicadoresOut{Fuente: src, Total: len(valores), Valores: make([]IndicadorValor, 0, len(valores))}
		var sb strings.Builder
		sb.WriteString("## Indicadores Económicos de Chile (hoy)\n\n")
		sb.WriteString(text.MDHeader("Indicador", "Valor", "Unidad", "Fecha"))
		for _, m := range valores {
			sb.WriteString(text.MDRow(mindicador.Label[m.Codigo]+" (`"+m.Codigo+"`)", strconv.FormatFloat(m.Valor, 'f', -1, 64), m.UnidadMedida, truncateDate(m.Fecha)))
			st.Valores = append(st.Valores, IndicadorValor{Codigo: m.Codigo, Nombre: mindicador.Label[m.Codigo], Valor: m.Valor, Unidad: m.UnidadMedida, Fecha: truncateDate(m.Fecha)})
		}
		fmt.Fprintf(&sb, "\nFuente: %s\n", src)
		return structResult(st, sb.String()), nil
	}

	resp, src, err := h.Indicadores.Serie(ctx, indicador, anio)
	if errors.Is(err, mindicador.ErrDecode) {
		return errMsg("respuesta inesperada de %s", src), nil
	}
	if err != nil {
		return errResult(err), nil
	}
	st := IndicadoresOut{Fuente: src, Indicador: indicador, Anio: anio, Nombre: resp.Nombre, Unidad: resp.UnidadMedida, Total: len(resp.Serie), Valores: []IndicadorValor{}}
	if len(resp.Serie) == 0 {
		return structResult(st, fmt.Sprintf("Sin datos de %s para %s (fuente: %s).", indicador, text.OrDefault(anio, "hoy"), src)), nil
	}
	sort.SliceStable(resp.Serie, func(i, j int) bool { return resp.Serie[i].Fecha > resp.Serie[j].Fecha })

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s (%s)\n", resp.Nombre, resp.UnidadMedida)
	n := min(len(resp.Serie), 30)
	fmt.Fprintf(&sb, "Registros: %d", len(resp.Serie))
	if n < len(resp.Serie) {
		fmt.Fprintf(&sb, " (mostrando los %d más recientes)", n)
	}
	sb.WriteString("\n\n" + text.MDHeader("Fecha", "Valor"))
	for _, s := range resp.Serie[:n] {
		sb.WriteString(text.MDRow(truncateDate(s.Fecha), strconv.FormatFloat(s.Valor, 'f', -1, 64)))
		st.Valores = append(st.Valores, IndicadorValor{Valor: s.Valor, Fecha: truncateDate(s.Fecha)})
	}
	fmt.Fprintf(&sb, "\nFuente: %s\n", src)
	return structResult(st, sb.String()), nil
}

func truncateDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}
