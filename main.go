package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const apiBase = "https://datos.gob.cl/api/action"

func main() {
	s := server.NewMCPServer("datos-gob-cl", "1.0.0")

	// --- datos.gob.cl (CKAN) ---

	s.AddTool(mcp.NewTool("buscar_datasets",
		mcp.WithDescription("Busca datasets en datos.gob.cl por palabra clave o categoría. Úsalo primero para descubrir qué datos existen."),
		mcp.WithString("q", mcp.Required(), mcp.Description("Término de búsqueda, ej: 'empresas', 'precios', 'salud'")),
		mcp.WithString("categoria", mcp.Description("Filtrar por categoría: gobierno, salud, finanzas, educacion, negocios, transporte, seguridad, etc.")),
		mcp.WithNumber("limite", mcp.Description("Cantidad de resultados (default 5)")),
	), buscarDatasets)

	s.AddTool(mcp.NewTool("ver_recurso",
		mcp.WithDescription("Muestra el esquema (campos y tipos) y una muestra de datos de un recurso. Úsalo después de buscar_datasets para entender la estructura antes de consultar."),
		mcp.WithString("resource_id", mcp.Required(), mcp.Description("ID del recurso (UUID)")),
		mcp.WithNumber("muestra", mcp.Description("Cantidad de registros de muestra (default 3)")),
	), verRecurso)

	s.AddTool(mcp.NewTool("consultar",
		mcp.WithDescription("Consulta datos de un recurso con filtros, búsqueda de texto y paginación. Es la herramienta principal de consulta."),
		mcp.WithString("resource_id", mcp.Required(), mcp.Description("ID del recurso (UUID)")),
		mcp.WithString("q", mcp.Description("Búsqueda de texto libre en todos los campos")),
		mcp.WithString("filtros", mcp.Description("Filtros exactos como JSON, ej: {\"Region\":\"13\",\"Tipo\":\"SpA\"}")),
		mcp.WithString("campos", mcp.Description("Campos a retornar separados por coma, ej: 'Nombre,RUT,Capital'")),
		mcp.WithString("ordenar", mcp.Description("Ordenamiento, ej: 'Capital desc'")),
		mcp.WithNumber("limite", mcp.Description("Cantidad de registros (default 20, max 500)")),
		mcp.WithNumber("offset", mcp.Description("Offset para paginación")),
	), consultar)

	s.AddTool(mcp.NewTool("buscar_empresa",
		mcp.WithDescription("Atajo para buscar empresas en el Registro de Empresas y Sociedades por RUT o nombre."),
		mcp.WithString("rut", mcp.Description("RUT de la empresa, ej: '78325537-5'")),
		mcp.WithString("nombre", mcp.Description("Nombre o razón social (búsqueda parcial)")),
		mcp.WithString("anio", mcp.Description("Año de constitución: 2024, 2025, 2026 (default 2026)")),
	), buscarEmpresa)

	// --- Indicadores Económicos (mindicador.cl) ---

	s.AddTool(mcp.NewTool("indicadores_economicos",
		mcp.WithDescription("Obtiene indicadores económicos de Chile: UF, dólar, euro, UTM, IPC, TPM, IMACEC, libra de cobre, bitcoin. Útil para cruzar con datos de empresas y analizar contexto económico."),
		mcp.WithString("indicador", mcp.Description("Indicador específico: uf, dolar, euro, utm, ipc, tpm, imacec, libra_cobre, bitcoin, ivp. Si no se especifica, retorna todos.")),
		mcp.WithString("anio", mcp.Description("Año para serie histórica, ej: '2026'. Si no se especifica, retorna valores del día.")),
	), indicadoresEconomicos)

	// --- Sismos (api.gael.cloud) ---

	s.AddTool(mcp.NewTool("sismos",
		mcp.WithDescription("Obtiene los últimos sismos registrados en Chile con magnitud, profundidad y ubicación. Útil para cruzar con datos regionales de empresas y construcción."),
	), ultimosSismos)

	// --- Calidad del Aire (SINCA - Ministerio del Medio Ambiente) ---

	s.AddTool(mcp.NewTool("calidad_aire",
		mcp.WithDescription("Obtiene datos en tiempo real de calidad del aire de las estaciones de monitoreo en Chile. Muestra MP2.5, MP10, SO2, NO2, CO. Útil para cruzar con datos regionales de empresas e identificar zonas saturadas."),
		mcp.WithString("region", mcp.Description("Filtrar por nombre de región, ej: 'Metropolitana', 'Valparaíso', 'Biobío'")),
		mcp.WithString("comuna", mcp.Description("Filtrar por comuna, ej: 'Santiago', 'Temuco'")),
	), calidadAire)

	// --- Farmacias de turno (datos.gob.cl) ---

	s.AddTool(mcp.NewTool("farmacias_turno",
		mcp.WithDescription("Busca farmacias de turno en Chile por comuna o región. Datos del Ministerio de Salud."),
		mcp.WithString("comuna", mcp.Description("Nombre de la comuna, ej: 'santiago', 'providencia'")),
		mcp.WithString("region", mcp.Description("Número de región, ej: '13' para Metropolitana")),
		mcp.WithNumber("limite", mcp.Description("Cantidad de resultados (default 10)")),
	), farmaciasTurno)

	// --- Mercado Público / ChileCompra ---

	s.AddTool(mcp.NewTool("licitaciones",
		mcp.WithDescription("Busca licitaciones públicas en Mercado Público (ChileCompra). Permite encontrar oportunidades de negocio con el Estado por fecha, estado o código. Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("fecha", mcp.Description("Fecha en formato ddmmaaaa, ej: '25032026' para 25 de marzo 2026")),
		mcp.WithString("estado", mcp.Description("Estado: activas, publicada, cerrada, desierta, adjudicada, revocada, suspendida")),
		mcp.WithString("codigo", mcp.Description("Código específico de licitación, ej: '1509-5-L114'")),
		mcp.WithString("codigo_organismo", mcp.Description("Código del organismo público comprador")),
		mcp.WithString("codigo_proveedor", mcp.Description("Código del proveedor")),
	), buscarLicitaciones)

	s.AddTool(mcp.NewTool("ordenes_compra",
		mcp.WithDescription("Busca órdenes de compra en Mercado Público. Muestra compras ya realizadas por organismos del Estado. Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("fecha", mcp.Description("Fecha en formato ddmmaaaa, ej: '25032026'")),
		mcp.WithString("estado", mcp.Description("Estado: enviadaproveedor, aceptada, cancelada, recepcionconforme, pendienterecepcion")),
		mcp.WithString("codigo", mcp.Description("Código específico de orden de compra")),
		mcp.WithString("codigo_organismo", mcp.Description("Código del organismo público comprador")),
	), buscarOrdenesCompra)

	s.AddTool(mcp.NewTool("proveedor_estado",
		mcp.WithDescription("Busca un proveedor del Estado por RUT en Mercado Público. Retorna su código para consultar licitaciones y órdenes de compra asociadas. Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("rut", mcp.Required(), mcp.Description("RUT del proveedor con formato 70.017.820-k")),
	), buscarProveedor)

	s.AddTool(mcp.NewTool("organismos_publicos",
		mcp.WithDescription("Lista todos los organismos públicos registrados en Mercado Público con sus códigos. Útil para filtrar licitaciones por organismo. Requiere MERCADO_PUBLICO_TICKET en env."),
	), listarOrganismos)

	if err := server.NewStdioServer(s).Listen(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
}

// ===================== datos.gob.cl Handlers =====================

func buscarDatasets(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	q := stringArg(req, "q")
	cat := stringArg(req, "categoria")
	limite := intArg(req, "limite", 5)

	params := url.Values{"q": {q}, "rows": {fmt.Sprint(limite)}}
	if cat != "" {
		params.Set("fq", "groups:"+cat)
	}

	body, err := apiGet("package_search", params)
	if err != nil {
		return errResult(err), nil
	}

	var resp struct {
		Result struct {
			Count   int `json:"count"`
			Results []struct {
				Title        string `json:"title"`
				Name         string `json:"name"`
				Notes        string `json:"notes"`
				Organization struct {
					Title string `json:"title"`
				} `json:"organization"`
				Resources []struct {
					ID              string `json:"id"`
					Name            string `json:"name"`
					Format          string `json:"format"`
					DatastoreActive bool   `json:"datastore_active"`
				} `json:"resources"`
			} `json:"results"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Encontrados: %d datasets\n\n", resp.Result.Count)
	for _, ds := range resp.Result.Results {
		fmt.Fprintf(&sb, "## %s\n", ds.Title)
		fmt.Fprintf(&sb, "Publicado por: %s\n", ds.Organization.Title)
		if len(ds.Notes) > 200 {
			ds.Notes = ds.Notes[:200] + "..."
		}
		fmt.Fprintf(&sb, "Descripción: %s\n", ds.Notes)
		queryable := 0
		for _, r := range ds.Resources {
			if r.DatastoreActive {
				queryable++
				fmt.Fprintf(&sb, "  -> %s [%s] resource_id: %s\n", r.Name, r.Format, r.ID)
			}
		}
		if queryable == 0 {
			fmt.Fprintf(&sb, "  (sin recursos consultables via API — solo descarga directa)\n")
		}
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func verRecurso(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rid := stringArg(req, "resource_id")
	muestra := intArg(req, "muestra", 3)

	body, err := apiGet("datastore_search", url.Values{
		"resource_id": {rid},
		"limit":       {fmt.Sprint(muestra)},
	})
	if err != nil {
		return errResult(err), nil
	}

	var resp dsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Total registros: %d\n\n", resp.Result.Total)
	fmt.Fprintf(&sb, "### Campos\n")
	for _, f := range resp.Result.Fields {
		if f.ID == "_id" || f.ID == "_full_text" || f.ID == "rank" {
			continue
		}
		fmt.Fprintf(&sb, "- **%s** (%s)\n", f.ID, f.Type)
	}
	sb.WriteString("\n### Muestra\n")
	for i, rec := range resp.Result.Records {
		fmt.Fprintf(&sb, "\nRegistro %d:\n", i+1)
		for _, f := range resp.Result.Fields {
			if f.ID == "_id" || f.ID == "_full_text" || f.ID == "rank" {
				continue
			}
			if v, ok := rec[f.ID]; ok && v != nil {
				fmt.Fprintf(&sb, "  %s: %v\n", f.ID, v)
			}
		}
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func consultar(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rid := stringArg(req, "resource_id")
	q := stringArg(req, "q")
	filtros := stringArg(req, "filtros")
	campos := stringArg(req, "campos")
	ordenar := stringArg(req, "ordenar")
	limite := intArg(req, "limite", 20)
	offset := intArg(req, "offset", 0)

	if limite > 500 {
		limite = 500
	}

	params := url.Values{
		"resource_id": {rid},
		"limit":       {fmt.Sprint(limite)},
	}
	if q != "" {
		params.Set("q", q)
	}
	if filtros != "" {
		params.Set("filters", filtros)
	}
	if campos != "" {
		params.Set("fields", campos)
	}
	if ordenar != "" {
		params.Set("sort", ordenar)
	}
	if offset > 0 {
		params.Set("offset", fmt.Sprint(offset))
	}

	body, err := apiGet("datastore_search", params)
	if err != nil {
		return errResult(err), nil
	}

	var resp dsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Total: %d | Mostrando: %d (offset %d)\n\n", resp.Result.Total, len(resp.Result.Records), offset)

	var fields []string
	for _, f := range resp.Result.Fields {
		if f.ID == "_id" || f.ID == "_full_text" || f.ID == "rank" {
			continue
		}
		fields = append(fields, f.ID)
	}

	for i, rec := range resp.Result.Records {
		fmt.Fprintf(&sb, "--- Registro %d ---\n", offset+i+1)
		for _, fname := range fields {
			if v, ok := rec[fname]; ok && v != nil {
				fmt.Fprintf(&sb, "  %s: %v\n", fname, v)
			}
		}
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}

var empresaResources = map[string]string{
	"2026": "472de7b5-384f-452d-9da5-2928689d8f2f",
	"2025": "71c8e355-226a-461e-809a-870c2275a178",
	"2024": "42ee8c8c-59cf-42e4-89af-ec19a87dbf8d",
	"2013": "fd2b91b0-eb8e-45f1-98d0-1f3316bb6468",
}

func buscarEmpresa(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rut := stringArg(req, "rut")
	nombre := stringArg(req, "nombre")
	anio := stringArg(req, "anio")
	if anio == "" {
		anio = "2026"
	}

	if rut == "" && nombre == "" {
		return errResult(fmt.Errorf("debes especificar rut o nombre")), nil
	}

	rid, ok := empresaResources[anio]
	if !ok {
		return errResult(fmt.Errorf("año no disponible, usar: 2024, 2025 o 2026")), nil
	}

	params := url.Values{
		"resource_id": {rid},
		"limit":       {"10"},
	}

	if rut != "" {
		params.Set("filters", fmt.Sprintf(`{"RUT":"%s"}`, rut))
	} else {
		params.Set("q", nombre)
	}

	body, err := apiGet("datastore_search", params)
	if err != nil {
		return errResult(err), nil
	}

	var resp dsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	if resp.Result.Total == 0 {
		fmt.Fprintf(&sb, "No se encontraron empresas en el registro %s.\n", anio)
		fmt.Fprintf(&sb, "Nota: el registro solo contiene constituciones del año %s.\n", anio)
		return mcp.NewToolResultText(sb.String()), nil
	}

	fmt.Fprintf(&sb, "Encontradas: %d empresas (registro %s)\n\n", resp.Result.Total, anio)
	for _, rec := range resp.Result.Records {
		fmt.Fprintf(&sb, "**%v**\n", rec["Razon Social"])
		fmt.Fprintf(&sb, "  RUT: %v\n", rec["RUT"])
		fmt.Fprintf(&sb, "  Tipo: %v\n", rec["Codigo de sociedad"])
		fmt.Fprintf(&sb, "  Capital: $%v\n", rec["Capital"])
		fmt.Fprintf(&sb, "  Comuna: %v (Región %v)\n", rec["Comuna Tributaria"], rec["Region Tributaria"])
		fmt.Fprintf(&sb, "  Constitución: %v\n", rec["Fecha de actuacion (1era firma)"])
		fmt.Fprintf(&sb, "  Aprobación SII: %v\n", rec["Fecha de aprobacion x SII"])
		fmt.Fprintf(&sb, "  Tipo actuación: %v\n\n", rec["Tipo de actuacion"])
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// ===================== Indicadores Económicos (mindicador.cl) =====================

func indicadoresEconomicos(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	indicador := stringArg(req, "indicador")
	anio := stringArg(req, "anio")

	var apiURL string
	if indicador != "" && anio != "" {
		apiURL = fmt.Sprintf("https://mindicador.cl/api/%s/%s", indicador, anio)
	} else if indicador != "" {
		apiURL = fmt.Sprintf("https://mindicador.cl/api/%s", indicador)
	} else {
		apiURL = "https://mindicador.cl/api"
	}

	body, err := httpGet(apiURL)
	if err != nil {
		return errResult(err), nil
	}

	// Si es consulta de todos los indicadores (sin indicador específico ni año)
	if indicador == "" && anio == "" {
		var resp map[string]any
		if err := json.Unmarshal(body, &resp); err != nil {
			return errResult(err), nil
		}

		var sb strings.Builder
		sb.WriteString("## Indicadores Económicos de Chile (hoy)\n\n")
		indicadores := []struct{ key, label string }{
			{"uf", "UF"},
			{"dolar", "Dólar Observado"},
			{"euro", "Euro"},
			{"utm", "UTM"},
			{"ipc", "IPC"},
			{"tpm", "TPM"},
			{"imacec", "IMACEC"},
			{"libra_cobre", "Libra de Cobre"},
			{"bitcoin", "Bitcoin"},
			{"ivp", "IVP"},
		}
		for _, ind := range indicadores {
			if data, ok := resp[ind.key]; ok {
				if m, ok := data.(map[string]any); ok {
					valor := m["valor"]
					unidad := m["unidad_medida"]
					fmt.Fprintf(&sb, "- **%s**: %v (%v)\n", ind.label, valor, unidad)
				}
			}
		}
		return mcp.NewToolResultText(sb.String()), nil
	}

	// Serie histórica o indicador específico del día
	var resp struct {
		Codigo       string `json:"codigo"`
		Nombre       string `json:"nombre"`
		UnidadMedida string `json:"unidad_medida"`
		Serie        []struct {
			Fecha string  `json:"fecha"`
			Valor float64 `json:"valor"`
		} `json:"serie"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s (%s)\n", resp.Nombre, resp.UnidadMedida)
	fmt.Fprintf(&sb, "Registros: %d\n\n", len(resp.Serie))

	limit := len(resp.Serie)
	if limit > 30 {
		limit = 30
		sb.WriteString("(mostrando últimos 30 valores)\n\n")
	}
	for i := 0; i < limit; i++ {
		s := resp.Serie[i]
		fecha := s.Fecha
		if len(fecha) >= 10 {
			fecha = fecha[:10]
		}
		fmt.Fprintf(&sb, "- %s: **%.2f**\n", fecha, s.Valor)
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// ===================== Sismos (api.gael.cloud) =====================

func ultimosSismos(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	body, err := httpGet("https://api.gael.cloud/general/public/sismos")
	if err != nil {
		return errResult(err), nil
	}

	var sismos []struct {
		Fecha          string `json:"Fecha"`
		Profundidad    string `json:"Profundidad"`
		Magnitud       string `json:"Magnitud"`
		RefGeografica  string `json:"RefGeografica"`
		FechaUpdate    string `json:"FechaUpdate"`
	}
	if err := json.Unmarshal(body, &sismos); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Últimos Sismos en Chile (%d registros)\n\n", len(sismos))
	for _, s := range sismos {
		fmt.Fprintf(&sb, "- **Mag %s** | Prof: %s km | %s | %s\n", s.Magnitud, s.Profundidad, s.RefGeografica, s.Fecha)
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// ===================== Calidad del Aire (SINCA) =====================

func calidadAire(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	regionFilter := strings.ToLower(stringArg(req, "region"))
	comunaFilter := strings.ToLower(stringArg(req, "comuna"))

	body, err := httpGet("https://sinca.mma.gob.cl/index.php/json/listadomapa2k19/")
	if err != nil {
		return errResult(err), nil
	}

	var estaciones []struct {
		Nombre       string  `json:"nombre"`
		Key          string  `json:"key"`
		Latitud      float64 `json:"latitud"`
		Longitud     float64 `json:"longitud"`
		Comuna       string  `json:"comuna"`
		Red          string  `json:"red"`
		Region       string  `json:"region"`
		RegionIndex  int     `json:"regionindex"`
		Calificacion string  `json:"calificacion"`
		Empresa      string  `json:"empresa"`
		Realtime     []struct {
			Code     string `json:"code"`
			Name     string `json:"name"`
			Datetime string `json:"datetime"`
			TableRow struct {
				Color      string  `json:"color"`
				Status     string  `json:"status"`
				StatusCode int     `json:"statuscode"`
				Datetime   string  `json:"datetime"`
				Parameter  string  `json:"parameter"`
				Movil      string  `json:"movil"`
				Value      float64 `json:"value"`
				Unit       string  `json:"unit"`
				Icap       float64 `json:"icap"`
				IcapText   string  `json:"icapText"`
			} `json:"tableRow"`
		} `json:"realtime"`
	}
	if err := json.Unmarshal(body, &estaciones); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	count := 0
	for _, e := range estaciones {
		if regionFilter != "" && !strings.Contains(strings.ToLower(e.Region), regionFilter) {
			continue
		}
		if comunaFilter != "" && !strings.Contains(strings.ToLower(e.Comuna), comunaFilter) {
			continue
		}
		count++
		fmt.Fprintf(&sb, "## %s (%s, %s)\n", e.Nombre, e.Comuna, e.Region)
		fmt.Fprintf(&sb, "Red: %s | Empresa: %s\n", e.Red, e.Empresa)
		for _, rt := range e.Realtime {
			tr := rt.TableRow
			if tr.Parameter == "" {
				continue
			}
			fmt.Fprintf(&sb, "- %s: **%.1f %s** | Estado: %s | ICAP: %.0f\n",
				tr.Parameter, tr.Value, tr.Unit, tr.Status, tr.Icap)
		}
		sb.WriteString("\n")
	}

	header := fmt.Sprintf("## Calidad del Aire — %d estaciones\n\n", count)
	return mcp.NewToolResultText(header + sb.String()), nil
}

// ===================== Farmacias de turno (datos.gob.cl CKAN) =====================

const farmaciasResourceID = "48e8d293-0580-4dd9-a7c1-1cfa4e89faae"

func farmaciasTurno(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	comuna := stringArg(req, "comuna")
	region := stringArg(req, "region")
	limite := intArg(req, "limite", 10)

	params := url.Values{
		"resource_id": {farmaciasResourceID},
		"limit":       {fmt.Sprint(limite)},
	}

	if comuna != "" {
		params.Set("q", comuna)
	}
	if region != "" {
		params.Set("filters", fmt.Sprintf(`{"fk_region":"%s"}`, region))
	}

	body, err := apiGet("datastore_search", params)
	if err != nil {
		return errResult(err), nil
	}

	var resp dsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Farmacias de Turno — %d resultados\n\n", resp.Result.Total)
	for _, rec := range resp.Result.Records {
		fmt.Fprintf(&sb, "**%v**\n", rec["local_nombre"])
		fmt.Fprintf(&sb, "  Dirección: %v, %v\n", rec["local_direccion"], rec["comuna_nombre"])
		fmt.Fprintf(&sb, "  Teléfono: %v\n", rec["local_telefono"])
		fmt.Fprintf(&sb, "  Horario: %v - %v\n", rec["funcionamiento_hora_apertura"], rec["funcionamiento_hora_cierre"])
		fmt.Fprintf(&sb, "  Día: %v\n\n", rec["funcionamiento_dia"])
	}
	return mcp.NewToolResultText(sb.String()), nil
}

// ===================== Mercado Público / ChileCompra =====================

const mpBase = "https://api.mercadopublico.cl/servicios/v1/publico"

func mpTicket() string {
	return os.Getenv("MERCADO_PUBLICO_TICKET")
}

func buscarLicitaciones(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticket := mpTicket()
	if ticket == "" {
		return errResult(fmt.Errorf("MERCADO_PUBLICO_TICKET no configurado. Solicita tu ticket en https://api.mercadopublico.cl con Clave Única")), nil
	}

	fecha := stringArg(req, "fecha")
	estado := stringArg(req, "estado")
	codigo := stringArg(req, "codigo")
	codOrg := stringArg(req, "codigo_organismo")
	codProv := stringArg(req, "codigo_proveedor")

	params := url.Values{"ticket": {ticket}}
	if fecha != "" {
		params.Set("fecha", fecha)
	}
	if estado != "" {
		params.Set("estado", estado)
	}
	if codigo != "" {
		params.Set("codigo", codigo)
	}
	if codOrg != "" {
		params.Set("CodigoOrganismo", codOrg)
	}
	if codProv != "" {
		params.Set("CodigoProveedor", codProv)
	}

	body, err := httpGet(mpBase + "/licitaciones.json?" + params.Encode())
	if err != nil {
		return errResult(err), nil
	}

	var resp struct {
		Cantidad int `json:"Cantidad"`
		Listado  []struct {
			CodigoExterno    string `json:"CodigoExterno"`
			Nombre           string `json:"Nombre"`
			CodigoEstado     int    `json:"CodigoEstado"`
			Estado           string `json:"Estado,omitempty"`
			Descripcion      string `json:"Descripcion"`
			FechaCierre      string `json:"FechaCierre"`
			Comprador        string `json:"NombreOrganismo,omitempty"`
			Tipo             string `json:"Tipo,omitempty"`
			TipoConvocatoria string `json:"TipoConvocatoria,omitempty"`
			Moneda           string `json:"Moneda,omitempty"`
			Estimacion       float64 `json:"MontoEstimado,omitempty"`
		} `json:"Listado"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		// Intentar parsear error de la API
		var apiErr struct {
			Codigo  int    `json:"Codigo"`
			Mensaje string `json:"Mensaje"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Mensaje != "" {
			return errResult(fmt.Errorf("API Mercado Público: %s", apiErr.Mensaje)), nil
		}
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Licitaciones — %d resultados\n\n", resp.Cantidad)
	for _, l := range resp.Listado {
		fmt.Fprintf(&sb, "### %s\n", l.Nombre)
		fmt.Fprintf(&sb, "- Código: **%s**\n", l.CodigoExterno)
		if l.Comprador != "" {
			fmt.Fprintf(&sb, "- Organismo: %s\n", l.Comprador)
		}
		desc := l.Descripcion
		if len(desc) > 300 {
			desc = desc[:300] + "..."
		}
		fmt.Fprintf(&sb, "- Descripción: %s\n", desc)
		if l.FechaCierre != "" {
			fmt.Fprintf(&sb, "- Cierre: %s\n", l.FechaCierre)
		}
		if l.Estimacion > 0 {
			fmt.Fprintf(&sb, "- Monto estimado: $%.0f %s\n", l.Estimacion, l.Moneda)
		}
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func buscarOrdenesCompra(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticket := mpTicket()
	if ticket == "" {
		return errResult(fmt.Errorf("MERCADO_PUBLICO_TICKET no configurado. Solicita tu ticket en https://api.mercadopublico.cl con Clave Única")), nil
	}

	fecha := stringArg(req, "fecha")
	estado := stringArg(req, "estado")
	codigo := stringArg(req, "codigo")
	codOrg := stringArg(req, "codigo_organismo")

	params := url.Values{"ticket": {ticket}}
	if fecha != "" {
		params.Set("fecha", fecha)
	}
	if estado != "" {
		params.Set("estado", estado)
	}
	if codigo != "" {
		params.Set("codigo", codigo)
	}
	if codOrg != "" {
		params.Set("CodigoOrganismo", codOrg)
	}

	body, err := httpGet(mpBase + "/ordenesdecompra.json?" + params.Encode())
	if err != nil {
		return errResult(err), nil
	}

	var resp struct {
		Cantidad int `json:"Cantidad"`
		Listado  []struct {
			Codigo       string  `json:"Codigo"`
			Nombre       string  `json:"Nombre"`
			Estado       string  `json:"Estado,omitempty"`
			Proveedor    string  `json:"NombreProveedor,omitempty"`
			Organismo    string  `json:"NombreOrganismo,omitempty"`
			Fecha        string  `json:"Fechaenvio,omitempty"`
			Total        float64 `json:"TotalNeto,omitempty"`
			Moneda       string  `json:"TipoMoneda,omitempty"`
		} `json:"Listado"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		var apiErr struct {
			Codigo  int    `json:"Codigo"`
			Mensaje string `json:"Mensaje"`
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Mensaje != "" {
			return errResult(fmt.Errorf("API Mercado Público: %s", apiErr.Mensaje)), nil
		}
		return errResult(err), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Órdenes de Compra — %d resultados\n\n", resp.Cantidad)
	for _, o := range resp.Listado {
		fmt.Fprintf(&sb, "- **%s** | %s\n", o.Codigo, o.Nombre)
		if o.Organismo != "" {
			fmt.Fprintf(&sb, "  Organismo: %s\n", o.Organismo)
		}
		if o.Proveedor != "" {
			fmt.Fprintf(&sb, "  Proveedor: %s\n", o.Proveedor)
		}
		if o.Total > 0 {
			fmt.Fprintf(&sb, "  Total: $%.0f %s\n", o.Total, o.Moneda)
		}
		if o.Estado != "" {
			fmt.Fprintf(&sb, "  Estado: %s\n", o.Estado)
		}
		sb.WriteString("\n")
	}
	return mcp.NewToolResultText(sb.String()), nil
}

func buscarProveedor(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticket := mpTicket()
	if ticket == "" {
		return errResult(fmt.Errorf("MERCADO_PUBLICO_TICKET no configurado")), nil
	}

	rut := stringArg(req, "rut")
	params := url.Values{
		"ticket":                {ticket},
		"rutempresaproveedor":   {rut},
	}

	body, err := httpGet(mpBase + "/Empresas/BuscarProveedor?" + params.Encode())
	if err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	sb.WriteString("## Proveedor del Estado\n\n")

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	formatted, _ := json.MarshalIndent(resp, "", "  ")
	sb.Write(formatted)
	return mcp.NewToolResultText(sb.String()), nil
}

func listarOrganismos(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ticket := mpTicket()
	if ticket == "" {
		return errResult(fmt.Errorf("MERCADO_PUBLICO_TICKET no configurado")), nil
	}

	params := url.Values{"ticket": {ticket}}
	body, err := httpGet(mpBase + "/Empresas/BuscarComprador?" + params.Encode())
	if err != nil {
		return errResult(err), nil
	}

	var sb strings.Builder
	sb.WriteString("## Organismos Públicos en Mercado Público\n\n")

	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return errResult(err), nil
	}

	formatted, _ := json.MarshalIndent(resp, "", "  ")
	sb.Write(formatted)
	return mcp.NewToolResultText(sb.String()), nil
}

// ===================== Helpers =====================

type dsResponse struct {
	Result struct {
		Total   int              `json:"total"`
		Fields  []dsField        `json:"fields"`
		Records []map[string]any `json:"records"`
	} `json:"result"`
}

type dsField struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// apiGet calls the datos.gob.cl CKAN API.
func apiGet(action string, params url.Values) ([]byte, error) {
	resp, err := http.Get(apiBase + "/" + action + "?" + params.Encode())
	if err != nil {
		return nil, fmt.Errorf("error HTTP: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API respondió %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return body, nil
}

// httpGet fetches any URL (for external APIs).
func httpGet(rawURL string) ([]byte, error) {
	resp, err := http.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("error HTTP: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error leyendo respuesta: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API respondió %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}
	return body, nil
}

func stringArg(req mcp.CallToolRequest, name string) string {
	return req.GetString(name, "")
}

func intArg(req mcp.CallToolRequest, name string, def int) int {
	return req.GetInt(name, def)
}

func errResult(err error) *mcp.CallToolResult {
	return mcp.NewToolResultError(err.Error())
}
