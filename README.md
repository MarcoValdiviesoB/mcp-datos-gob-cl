# mcp-datos-gob-cl

Servidor **MCP (Model Context Protocol)** en Go que le da a un agente de IA acceso
en vivo a **datos públicos de Chile**: los datasets de datos.gob.cl, el Registro de
Empresas y Sociedades, indicadores económicos, sismos, calidad del aire, farmacias de
turno y Mercado Público (ChileCompra).

Proyecto personal, independiente y no oficial. No está afiliado a los proveedores de datos.

## Por qué

La mayoría de las integraciones con datos.gob.cl solo exponen la **metadata** del
catálogo CKAN: te dicen que existe un dataset y dónde descargarlo. Este servidor además
**consulta los datos**: filtra, busca, ordena y pagina las filas de los recursos con
DataStore, y devuelve una tabla compacta que el modelo puede leer. Con eso un agente
responde preguntas que cruzan fuentes ("¿este proveedor del Estado cuándo se constituyó y
cuánto le han comprado este mes, en UF?") sin que tengas que bajar ningún CSV.

- Un binario estático, sin dependencias. Corre por stdio o como servidor HTTP.
- Todas las herramientas son de solo lectura.
- Funciona sin credenciales; solo Mercado Público pide un ticket gratuito.
- Timeouts, reintentos con backoff y fuentes de respaldo donde existen.

## Herramientas

> Empezá por **`catalogo`**: lista todas las fuentes y herramientas, su estado y lo que
> falta por cubrir.

| Herramienta | Fuente | Qué hace | Parámetros |
|---|---|---|---|
| `catalogo` | — | Índice de fuentes y herramientas con su estado (activo / requiere ticket) y lo pendiente. | — |
| `categorias_datasets` | datos.gob.cl | Mapa del portal: organismos (slug y nombre) y tags con cantidad de datasets. | `limite?` |
| `buscar_datasets` | datos.gob.cl | Busca o navega los ~3.200 datasets (q vacío = todos). Muestra `resource_id` o, si no hay DataStore, la URL de descarga. | `q?`, `organismo?`, `limite?`, `offset?` |
| `ver_recurso` | datos.gob.cl | Esquema (campos y tipos) y muestra de un recurso. | `resource_id`, `muestra?` |
| `consultar` | datos.gob.cl | Consulta las filas de un recurso con filtros exactos, texto libre, campos, orden y paginación. | `resource_id`, `q?`, `filtros?`, `campos?`, `ordenar?`, `limite?`, `offset?` |
| `buscar_empresa` | Registro de Empresas y Sociedades | Busca por RUT (con o sin puntos/guion) o nombre. Sin `anio` busca en todos los años consultables. | `rut?`, `nombre?`, `anio?`, `limite?`, `offset?` |
| `indicadores_economicos` | mindicador.cl (respaldo findic.cl) | UF, IVP, dólar, dólar acuerdo, euro, IPC, UTM, IMACEC, TPM, libra de cobre, desempleo, bitcoin. Con `anio`, serie de ese año. | `indicador?`, `anio?` |
| `sismos` | CSN vía api.xor.cl (respaldo api.gael.cloud) | Últimos sismos con magnitud, profundidad, coordenadas y hora de Chile/UTC. | `magnitud_minima?` |
| `calidad_aire` | SINCA (MMA) | MP2.5, MP10, SO2, NO2, CO y O3 en tiempo real por estación. Sin filtros, resumen por región. | `region?`, `comuna?` |
| `farmacias_turno` | Farmanet (MINSAL) | Farmacias de turno de hoy. `region` acepta número (`13`), romano (`XIII`) o nombre. | `comuna?`, `region?`, `limite?` |
| `licitaciones` | Mercado Público | Licitaciones por fecha (`ddmmaaaa`), estado, código, organismo o proveedor. Con `codigo`, el detalle. | `fecha?`, `estado?`, `codigo?`, `codigo_organismo?`, `codigo_proveedor?`, `limite?`, `pagina?` |
| `ordenes_compra` | Mercado Público | Órdenes de compra; con `codigo`, organismo, proveedor y montos. | `fecha?`, `estado?`, `codigo?`, `codigo_organismo?`, `codigo_proveedor?`, `limite?`, `pagina?` |
| `proveedor_estado` | Mercado Público | Código de proveedor del Estado a partir del RUT. | `rut` |
| `organismos_publicos` | Mercado Público | Organismos compradores con su código, filtrables por texto. | `q?`, `limite?`, `pagina?` |

Las herramientas de Mercado Público requieren `MERCADO_PUBLICO_TICKET`. Las respuestas
largas vienen paginadas y recortadas a ~20 KB, con aviso de cómo pedir la página siguiente.

Valores de `estado`: licitaciones `activas`, `publicada`, `cerrada`, `desierta`,
`adjudicada`, `revocada`, `suspendida`, `todos`; órdenes de compra `enviadaproveedor`,
`aceptada`, `cancelada`, `recepcionconforme`, `pendienterecepcion`, `todos`.

## Instalación

### Homebrew (macOS y Linux)

```bash
brew install --cask marcovaldiviesob/tap/mcp-datos-gob-cl
```

### Go

Requiere Go 1.25.5 o superior.

```bash
go install github.com/MarcoValdiviesoB/mcp-datos-gob-cl@latest
```

El binario queda en `$(go env GOPATH)/bin/mcp-datos-gob-cl`.

### Docker

```bash
docker pull ghcr.io/marcovaldiviesob/mcp-datos-gob-cl:latest
```

La imagen es multi-arch (amd64/arm64), basada en distroless y corre como usuario sin
privilegios. Por defecto habla MCP por stdio, así que hay que correrla con `-i`.

### Binario precompilado

Descargá el archivo para tu sistema desde
[Releases](https://github.com/MarcoValdiviesoB/mcp-datos-gob-cl/releases), verificá el
checksum contra `checksums.txt` y poné el binario en tu `PATH`:

```bash
shasum -a 256 -c checksums.txt --ignore-missing
tar -xzf mcp-datos-gob-cl_1.0.0_darwin_arm64.tar.gz
mv mcp-datos-gob-cl /usr/local/bin/
mcp-datos-gob-cl --version
```

En macOS, si Gatekeeper bloquea el binario descargado a mano:
`xattr -d com.apple.quarantine /usr/local/bin/mcp-datos-gob-cl`.

## Configuración

`MERCADO_PUBLICO_TICKET` es opcional en todos los clientes: sin él funcionan todas las
herramientas salvo las de Mercado Público.

### Claude Code

```bash
claude mcp add datos-gob-cl -e MERCADO_PUBLICO_TICKET=tu-ticket -- mcp-datos-gob-cl
```

Con Docker:

```bash
claude mcp add datos-gob-cl -e MERCADO_PUBLICO_TICKET=tu-ticket -- \
  docker run -i --rm -e MERCADO_PUBLICO_TICKET ghcr.io/marcovaldiviesob/mcp-datos-gob-cl
```

### Claude Desktop

En `claude_desktop_config.json` (macOS: `~/Library/Application Support/Claude/`;
Windows: `%APPDATA%\Claude\`):

```json
{
  "mcpServers": {
    "datos-gob-cl": {
      "command": "/opt/homebrew/bin/mcp-datos-gob-cl",
      "env": { "MERCADO_PUBLICO_TICKET": "tu-ticket" }
    }
  }
}
```

Claude Desktop no hereda tu `PATH`: usá la ruta absoluta (`which mcp-datos-gob-cl`).
Con Docker:

```json
{
  "mcpServers": {
    "datos-gob-cl": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "MERCADO_PUBLICO_TICKET", "ghcr.io/marcovaldiviesob/mcp-datos-gob-cl"],
      "env": { "MERCADO_PUBLICO_TICKET": "tu-ticket" }
    }
  }
}
```

### Cursor

En `~/.cursor/mcp.json` (global) o `.cursor/mcp.json` (por proyecto):

```json
{
  "mcpServers": {
    "datos-gob-cl": {
      "command": "mcp-datos-gob-cl",
      "env": { "MERCADO_PUBLICO_TICKET": "tu-ticket" }
    }
  }
}
```

### Modo HTTP remoto

Con `--http` el servidor expone MCP por streamable HTTP en `/mcp` y un chequeo de salud
en `/health`:

```bash
mcp-datos-gob-cl --http :8080
# o con Docker
docker run --rm -p 8080:8080 -e MERCADO_PUBLICO_TICKET=tu-ticket \
  ghcr.io/marcovaldiviesob/mcp-datos-gob-cl --http :8080

curl http://localhost:8080/health
```

Conectar clientes:

```bash
claude mcp add --transport http datos-gob-cl http://localhost:8080/mcp
```

```json
{ "mcpServers": { "datos-gob-cl": { "url": "http://localhost:8080/mcp" } } }
```

El modo HTTP no trae autenticación propia y el ticket de Mercado Público queda del lado
del servidor, compartido por todos los clientes. Si lo exponés fuera de tu máquina, ponelo
detrás de un proxy con autenticación.

## MERCADO_PUBLICO_TICKET

Las herramientas `licitaciones`, `ordenes_compra`, `proveedor_estado` y
`organismos_publicos` usan la API de Mercado Público, que pide un ticket:

1. Entrá a [api.mercadopublico.cl](https://api.mercadopublico.cl) y andá a la sección
   de solicitud de ticket.
2. Identificate con **Clave Única** y completá el formulario.
3. El ticket (un UUID) llega por correo. Pasalo como variable de entorno
   `MERCADO_PUBLICO_TICKET`.

La documentación de Mercado Público publica un **ticket de prueba**: sirve para
probar, pero es compartido por todo el mundo, choca con el límite de consultas
simultáneas y puede dejar de funcionar sin aviso. **No lo uses en producción**: pedí uno
propio, es gratis. El servidor nunca muestra el ticket en sus mensajes de error.

## Preguntas de ejemplo

Preguntas que cruzan fuentes y que un agente con este servidor puede responder:

1. "¿Qué hay disponible en este servidor y qué falta por cubrir?"
2. "Buscá al proveedor del Estado con RUT 76.xxx.xxx-x: ¿cuándo se constituyó según el
   Registro de Empresas, qué tipo de sociedad es y qué órdenes de compra recibió este mes?"
3. "Listá las licitaciones adjudicadas ayer por la Municipalidad de Temuco y expresá sus
   montos en UF al valor de hoy."
4. "¿Hubo algún sismo de magnitud 5 o más hoy? Si fue cerca de una ciudad, ¿qué
   farmacias de turno hay en esa región?"
5. "¿Cómo está la calidad del aire en Temuco ahora y qué farmacias de turno hay en la
   comuna?"
6. "¿Qué datasets publica la Dirección de Presupuestos? Mostrame las columnas del más
   reciente sobre ejecución presupuestaria y las 10 filas con mayor monto."
7. "¿Cuántas sociedades se constituyeron en 2025 con 'agrícola' en el nombre? Dame las
   primeras 20 con su capital."
8. "Compará la UF del primer día hábil de 2025 con la de hoy: ¿cuánto subió en pesos un
   arriendo de 15 UF?"
9. "Del dataset de establecimientos de salud, filtrá los de la Región de Valparaíso y
   cruzalos con las farmacias de turno de Viña del Mar."
10. "¿Qué órdenes de compra emitió hoy el Ministerio de Salud, a qué proveedores, y cuánto
    suman en dólares al valor observado?"

## Troubleshooting

**`indicadores_economicos` dice que usó findic.cl.** mindicador.cl está caído o lento
(timeout de 6 s). El servidor cae solo a findic.cl, que publica la misma API, y la
respuesta indica la fuente usada. No hay que hacer nada; si ambas fallan, reintentá en
unos minutos.

**Mercado Público responde "consultas simultáneas" o 429 (código 10500).** La API
rechaza peticiones paralelas con el mismo ticket. El servidor ya las serializa y reintenta
con backoff; si igual aparece, es porque el ticket se está usando en otro lado (el de
prueba, o uno compartido). Pedí un ticket propio.

**"MERCADO_PUBLICO_TICKET no configurado" o "no es válido".** La variable no llegó al
proceso. En Claude Desktop va dentro de `env` del servidor; con Docker, pasala con
`-e MERCADO_PUBLICO_TICKET`.

**Timeouts.** Cada fuente tiene un timeout por intento y reintentos ante
429/502/503/504: 6 s mindicador.cl, 8-10 s sismos y findic.cl, 15 s datos.gob.cl, SINCA y
Farmanet, 20 s Mercado Público. Si el error persiste, la fuente está caída: reintentá más
tarde. Si una consulta a
`consultar` tarda, bajá `limite` o pedí solo los `campos` que necesitás.

**`buscar_empresa` no encuentra una empresa de 2014-2023.** El Registro de Empresas de
esos años existe solo como CSV descargable, sin DataStore, así que no se puede consultar
por API. Con `anio` de ese rango el servidor devuelve la URL de descarga. Los años
consultables hoy son 2013 y 2024 en adelante. Además, el registro contiene las
constituciones y modificaciones de cada año, no todas las empresas de Chile.

**"El recurso no tiene DataStore".** No todos los recursos de datos.gob.cl son
consultables: muchos son solo archivos (XLSX, PDF, CSV sin cargar). La herramienta
devuelve la URL de descarga.

**`consultar` ordena raro ("10" antes que "9").** En campos de tipo text el orden es
alfabético, y los filtros comparan texto exacto. Revisá los tipos con `ver_recurso`.

**Claude Desktop no arranca el servidor.** Usá la ruta absoluta del binario y revisá los
logs en `~/Library/Logs/Claude/mcp*.log`.

## Comparación con otros servidores

Hay otros dos servidores MCP con datos públicos chilenos. Cada uno resuelve un problema
distinto:

| | **mcp-datos-gob-cl** (este) | [pipeworx-io/mcp-datos-cl](https://github.com/pipeworx-io/mcp-datos-cl) | [IanGuiloff/entes-mcp](https://github.com/IanGuiloff/entes-mcp) |
|---|---|---|---|
| Enfoque | Consultar datos públicos en vivo | Superficie CKAN genérica de datos.gob.cl | Mapa institucional y regulatorio de 31 entes |
| datos.gob.cl | Metadata **y filas** (DataStore: filtros, orden, paginación) | Solo metadata (`package_search`, `resource_show`, etc.) | Solo búsqueda de datasets (`package_search`) |
| Otras fuentes | Registro de Empresas, indicadores, sismos, calidad del aire, farmacias, Mercado Público (4 tools) | Ninguna en este pack; el gateway de Pipeworx suma muchas más | SII, Banco Central, indicadores, licitaciones, normativa SII/CMF, deep-links |
| Ejecución | Local (binario, Docker) o tu propio HTTP | Gateway hospedado por Pipeworx | Local (Python), con navegador opcional |
| Lenguaje | Go | JavaScript | Python |

Si necesitás saber **ante qué organismo, con qué potestad y qué trámite**, o razonamiento
jurídico, entes-mcp está mucho más completo. Si querés un endpoint hospedado sin instalar
nada y con acceso a cientos de fuentes globales, Pipeworx es más cómodo (a cambio de
sumar unas 30 meta-herramientas al contexto y depender de un tercero). Este servidor
apunta a otra cosa: leer las filas de los datasets y cruzarlas con indicadores, empresas
y compras públicas, corriendo en tu máquina.

## Desarrollo

```bash
go vet ./... && go test ./...
go build -ldflags "-X main.version=dev" -o mcp-datos-gob-cl .
goreleaser release --snapshot --clean   # prueba local del release, no publica nada
docker build -t mcp-datos-gob-cl .
```

Los releases se publican al pushear un tag `v*` (ver `.github/workflows/release.yml`).

## Fuentes de datos

- [datos.gob.cl](https://datos.gob.cl) — portal de datos abiertos del Estado (CKAN)
- Registro de Empresas y Sociedades (vía datos.gob.cl)
- [mindicador.cl](https://mindicador.cl) y [findic.cl](https://findic.cl) — indicadores económicos
- Centro Sismológico Nacional vía api.xor.cl y api.gael.cloud
- SINCA — Ministerio del Medio Ambiente
- Farmanet — Ministerio de Salud
- [Mercado Público](https://api.mercadopublico.cl) — ChileCompra

## Licencia

[MIT](LICENSE).

---

## English summary

**mcp-datos-gob-cl** is a Go MCP server that gives AI agents live, read-only access to
Chilean public data: the ~3,200 datasets on datos.gob.cl (it queries the actual rows
through CKAN's DataStore, not just catalogue metadata), the company registry, economic
indicators (UF, USD, UTM, CPI…, with automatic fallback), earthquakes, air quality,
on-duty pharmacies and the Mercado Público procurement API (needs a free
`MERCADO_PUBLICO_TICKET`). It ships as a single static binary (Homebrew, `go install`,
GitHub Releases) and as a Docker image at `ghcr.io/marcovaldiviesob/mcp-datos-gob-cl`,
and runs over stdio or streamable HTTP (`--http :8080`). Start with the `catalogo` tool.
Unofficial, MIT licensed.
