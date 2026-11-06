// Package tools registra las herramientas MCP y arma sus respuestas a partir de los clientes de internal/.
package tools

import (
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/ckan"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/farmanet"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/httpx"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mercadopublico"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/mindicador"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/sinca"
	"github.com/MarcoValdiviesoB/mcp-datos-gob-cl/internal/sismos"
)

const instrucciones = `Datos públicos de Chile en vivo (datos.gob.cl, indicadores económicos, sismos, calidad del aire, farmacias de turno y Mercado Público).
Empezá por 'catalogo' para ver qué hay y su estado. Para datos abiertos: categorias_datasets o buscar_datasets -> ver_recurso -> consultar.
Las respuestas largas vienen paginadas (limite/offset/pagina) y recortadas a ~20 KB. Todas las herramientas son de solo lectura.`

// Deps son los clientes de cada fuente. En los tests apuntan a servidores httptest.
type Deps struct {
	Version     string
	CKAN        *ckan.Client
	Indicadores *mindicador.Client
	Sismos      *sismos.Client
	SINCA       *sinca.Client
	Farmanet    *farmanet.Client
	MP          *mercadopublico.Client
	Now         func() time.Time // reloj para "hoy" (farmacias) y el año actual (indicadores)
}

// DefaultDeps arma los clientes contra las APIs reales.
func DefaultDeps(version, userAgent string) Deps {
	hc := httpx.New(userAgent)
	return Deps{
		Version:     version,
		CKAN:        ckan.New(hc),
		Indicadores: mindicador.New(hc),
		Sismos:      sismos.New(hc),
		SINCA:       sinca.New(hc),
		Farmanet:    farmanet.New(hc),
		MP:          mercadopublico.New(hc),
		Now:         time.Now,
	}
}

type handlers struct{ Deps }

// tool crea una herramienta de solo lectura que consulta fuentes externas, con título.
func tool(name, title string, opts ...mcp.ToolOption) mcp.Tool {
	opts = append(opts,
		mcp.WithTitleAnnotation(title),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
	)
	return mcp.NewTool(name, opts...)
}

// NewServer crea el servidor MCP con todas las herramientas registradas.
func NewServer(d Deps) *server.MCPServer {
	if d.Now == nil {
		d.Now = time.Now
	}
	h := &handlers{d}
	s := server.NewMCPServer("datos-gob-cl", d.Version,
		server.WithRecovery(),
		server.WithInstructions(instrucciones),
	)

	s.AddTool(tool("catalogo", "Catálogo de fuentes",
		mcp.WithDescription("Lista todas las fuentes de datos y herramientas disponibles, su estado (activo / requiere ticket / externo) y lo que falta por cubrir. Úsalo primero para descubrir qué puede hacer este servidor."),
	), h.catalogo)

	// --- datos.gob.cl (CKAN) ---

	s.AddTool(tool("buscar_datasets", "Buscar datasets en datos.gob.cl",
		mcp.WithDescription("Busca o navega los ~3.200 datasets de datos.gob.cl. Si 'q' va vacío, lista todos (paginado con 'offset'). Filtrable por organismo. Muestra el slug del dataset y del organismo; los recursos con resource_id se consultan con ver_recurso/consultar y los 'sin DataStore' traen su URL de descarga."),
		mcp.WithString("q", mcp.Description("Término de búsqueda (opcional). Vacío = navegar todos los datasets.")),
		mcp.WithString("organismo", mcp.Description("Slug del organismo publicador para filtrar, ej: 'direccion_de_presupuestos' (ver categorias_datasets).")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(100), mcp.Description("Resultados por página (default 10, máx 100)")),
		mcp.WithNumber("offset", mcp.Min(0), mcp.Description("Desde qué resultado (paginación), default 0")),
	), h.buscarDatasets)

	s.AddTool(tool("categorias_datasets", "Mapa de datos.gob.cl",
		mcp.WithDescription("Mapa de todo datos.gob.cl: organismos publicadores (slug y nombre) y tags más frecuentes con su cantidad de datasets. Úsalo para ver el panorama y luego filtrar buscar_datasets por organismo."),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(200), mcp.Description("Cuántos organismos y tags mostrar (default 30, máx 200)")),
	), h.categoriasDatasets)

	s.AddTool(tool("ver_recurso", "Ver esquema de un recurso",
		mcp.WithDescription("Muestra el esquema (campos y tipos) y una muestra de datos de un recurso. Úsalo después de buscar_datasets para entender la estructura antes de consultar. Si el recurso no tiene DataStore, indica la URL de descarga."),
		mcp.WithString("resource_id", mcp.Required(), mcp.Description("ID del recurso (UUID)")),
		mcp.WithNumber("muestra", mcp.Min(0), mcp.Max(50), mcp.Description("Cantidad de registros de muestra (default 3, máx 50; 0 = solo esquema)")),
	), h.verRecurso)

	s.AddTool(tool("consultar", "Consultar un recurso",
		mcp.WithDescription("Consulta datos de un recurso con filtros, búsqueda de texto y paginación; devuelve una tabla compacta. Los filtros comparan texto exacto (los números se convierten a string). En campos de tipo text, 'ordenar' es alfabético (\"10\" < \"9\")."),
		mcp.WithString("resource_id", mcp.Required(), mcp.Description("ID del recurso (UUID)")),
		mcp.WithString("q", mcp.Description("Búsqueda de texto libre en todos los campos")),
		mcp.WithString("filtros", mcp.Description("Filtros exactos como JSON, ej: {\"Region\":\"13\",\"Tipo\":\"SpA\"}. Acepta listas: {\"Region\":[\"5\",\"13\"]}")),
		mcp.WithString("campos", mcp.Description("Campos a retornar separados por coma, ej: 'Nombre,RUT,Capital'")),
		mcp.WithString("ordenar", mcp.Description("Ordenamiento, ej: 'Capital desc' (textual si el campo es text)")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(500), mcp.Description("Cantidad de registros (default 20, máx 500; la salida se recorta a ~20 KB)")),
		mcp.WithNumber("offset", mcp.Min(0), mcp.Description("Offset para paginación")),
		mcp.WithOutputSchema[ConsultaOut](),
	), h.consultar)

	s.AddTool(tool("buscar_empresa", "Buscar empresa (Registro de Empresas y Sociedades)",
		mcp.WithDescription("Busca empresas en el Registro de Empresas y Sociedades por RUT o nombre. Sin 'anio' busca en todos los años con datos consultables (hoy 2013, 2024, 2025 y 2026; 2014-2023 solo existen como CSV descargable). El registro contiene constituciones y modificaciones de cada año, no todas las empresas de Chile."),
		mcp.WithString("rut", mcp.Description("RUT de la empresa, con o sin puntos/guion, ej: '78.325.537-5' o '783255375'")),
		mcp.WithString("nombre", mcp.Description("Nombre o razón social (búsqueda parcial)")),
		mcp.WithString("anio", mcp.Pattern(`^\d{4}$`), mcp.Description("Año del registro (4 dígitos). Opcional: sin él busca en todos los años consultables.")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(100), mcp.Description("Resultados por página (default 10, máx 100)")),
		mcp.WithNumber("offset", mcp.Min(0), mcp.Max(900), mcp.Description("Desde qué resultado (paginación)")),
	), h.buscarEmpresa)

	// --- Indicadores Económicos (mindicador.cl, respaldo findic.cl) ---

	s.AddTool(tool("indicadores_economicos", "Indicadores económicos de Chile",
		mcp.WithDescription("Obtiene indicadores económicos de Chile: UF, dólar, euro, UTM, IPC, TPM, IMACEC, libra de cobre, desempleo, bitcoin. Fuente mindicador.cl con respaldo automático en findic.cl."),
		mcp.WithString("indicador", mcp.Enum(mindicador.Validos...), mcp.Description("Indicador específico. Si no se especifica, retorna todos los valores del día.")),
		mcp.WithString("anio", mcp.Pattern(`^\d{4}$`), mcp.Description("Año para serie histórica (4 dígitos, ej: '2025'). Requiere 'indicador'.")),
		mcp.WithOutputSchema[IndicadoresOut](),
	), h.indicadoresEconomicos)

	// --- Sismos (CSN vía api.xor.cl, respaldo api.gael.cloud) ---

	s.AddTool(tool("sismos", "Últimos sismos en Chile",
		mcp.WithDescription("Obtiene los últimos sismos registrados en Chile (Centro Sismológico Nacional) con magnitud, profundidad, coordenadas, hora de Chile y UTC."),
		mcp.WithNumber("magnitud_minima", mcp.Min(0), mcp.Max(10), mcp.Description("Solo sismos con magnitud mayor o igual, ej: 4")),
	), h.ultimosSismos)

	// --- Calidad del Aire (SINCA - Ministerio del Medio Ambiente) ---

	s.AddTool(tool("calidad_aire", "Calidad del aire (SINCA)",
		mcp.WithDescription("Datos en tiempo real de calidad del aire de las estaciones de monitoreo en Chile (MP2.5, MP10, SO2, NO2, CO, O3) con hora de medición. Sin filtros devuelve un resumen por región."),
		mcp.WithString("region", mcp.Description("Región por nombre o número, ej: 'Metropolitana', '13', 'Valparaíso', 'Biobío' (tildes y mayúsculas no importan)")),
		mcp.WithString("comuna", mcp.Description("Filtrar por comuna, ej: 'Santiago', 'Temuco'")),
	), h.calidadAire)

	// --- Farmacias de turno (Farmanet MINSAL) ---

	s.AddTool(tool("farmacias_turno", "Farmacias de turno",
		mcp.WithDescription("Busca farmacias de turno de hoy en Chile por comuna o región. Datos de Farmanet (Ministerio de Salud)."),
		mcp.WithString("comuna", mcp.Description("Nombre de la comuna, ej: 'santiago', 'providencia', 'Ñuñoa'")),
		mcp.WithString("region", mcp.Description("Región: número oficial ('13' = Metropolitana, '5' = Valparaíso), romano ('XIII') o nombre")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(100), mcp.Description("Cantidad de resultados (default 10, máx 100)")),
	), h.farmaciasTurno)

	// --- Mercado Público / ChileCompra ---

	s.AddTool(tool("licitaciones", "Licitaciones (Mercado Público)",
		mcp.WithDescription("Busca licitaciones públicas en Mercado Público (ChileCompra) por fecha, estado, código, organismo o proveedor. El listado trae código, nombre, estado y cierre; con 'codigo' trae el detalle (organismo, monto, fechas). Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("fecha", mcp.Description("Fecha en formato ddmmaaaa, ej: '25032026' para 25 de marzo 2026")),
		mcp.WithString("estado", mcp.Enum(mercadopublico.EstadosLicitacion...), mcp.Description("Estado de la licitación")),
		mcp.WithString("codigo", mcp.Description("Código específico de licitación, ej: '1509-5-L114' (devuelve el detalle)")),
		mcp.WithString("codigo_organismo", mcp.Description("Código del organismo público comprador (ver organismos_publicos)")),
		mcp.WithString("codigo_proveedor", mcp.Description("Código del proveedor (ver proveedor_estado)")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(100), mcp.Description("Resultados por página (default 25, máx 100)")),
		mcp.WithNumber("pagina", mcp.Min(1), mcp.Description("Página, desde 1")),
		mcp.WithOutputSchema[LicitacionesOut](),
	), h.buscarLicitaciones)

	s.AddTool(tool("ordenes_compra", "Órdenes de compra (Mercado Público)",
		mcp.WithDescription("Busca órdenes de compra en Mercado Público. El listado trae código, nombre y estado; con 'codigo' trae el detalle (organismo, proveedor, montos, fechas). Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("fecha", mcp.Description("Fecha en formato ddmmaaaa, ej: '25032026'")),
		mcp.WithString("estado", mcp.Enum(mercadopublico.EstadosOC...), mcp.Description("Estado de la orden de compra")),
		mcp.WithString("codigo", mcp.Description("Código específico de orden de compra (devuelve el detalle)")),
		mcp.WithString("codigo_organismo", mcp.Description("Código del organismo público comprador")),
		mcp.WithString("codigo_proveedor", mcp.Description("Código del proveedor (ver proveedor_estado)")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(100), mcp.Description("Resultados por página (default 25, máx 100)")),
		mcp.WithNumber("pagina", mcp.Min(1), mcp.Description("Página, desde 1")),
		mcp.WithOutputSchema[OrdenesOut](),
	), h.buscarOrdenesCompra)

	s.AddTool(tool("proveedor_estado", "Proveedor del Estado por RUT",
		mcp.WithDescription("Busca un proveedor del Estado por RUT en Mercado Público. Retorna su código para consultar licitaciones y órdenes de compra asociadas. Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("rut", mcp.Required(), mcp.Description("RUT del proveedor, con o sin puntos/guion, ej: '70.017.820-K' o '70017820k'")),
	), h.buscarProveedor)

	s.AddTool(tool("organismos_publicos", "Organismos públicos compradores",
		mcp.WithDescription("Lista los organismos públicos registrados en Mercado Público con sus códigos, filtrables por texto. Útil para filtrar licitaciones y órdenes por organismo. Requiere MERCADO_PUBLICO_TICKET en env."),
		mcp.WithString("q", mcp.Description("Texto a buscar en el nombre, ej: 'municipalidad de temuco' (tildes y mayúsculas no importan)")),
		mcp.WithNumber("limite", mcp.Min(1), mcp.Max(500), mcp.Description("Resultados por página (default 50, máx 500)")),
		mcp.WithNumber("pagina", mcp.Min(1), mcp.Description("Página, desde 1")),
	), h.listarOrganismos)

	return s
}
