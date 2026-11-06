---
name: datos-gob-cl
description: Consultar datos públicos de Chile en vivo con el servidor MCP datos-gob-cl — filas de datasets de datos.gob.cl, Registro de Empresas y Sociedades, indicadores económicos (UF, dólar, UTM, IPC), sismos, calidad del aire, farmacias de turno y Mercado Público (licitaciones, órdenes de compra, proveedores). Usala cuando pregunten por datos oficiales chilenos, un RUT de empresa o proveedor del Estado, compras públicas, o pidan cruzar varias de estas fuentes.
---

# datos-gob-cl

Guía para usar las herramientas del servidor MCP `datos-gob-cl` (todas de solo lectura).
Si las herramientas no aparecen, el servidor no está conectado: ver
https://github.com/MarcoValdiviesoB/mcp-datos-gob-cl#configuración.

## Qué herramienta usar

| Necesidad | Herramienta |
|---|---|
| No sé qué hay disponible | `catalogo` |
| Panorama de datos.gob.cl: qué organismos publican y sobre qué | `categorias_datasets` |
| Encontrar un dataset por tema u organismo | `buscar_datasets` |
| Ver columnas y tipos de un recurso antes de consultarlo | `ver_recurso` |
| Leer, filtrar u ordenar las filas de un recurso | `consultar` |
| Una empresa por RUT o nombre (constitución, tipo, capital) | `buscar_empresa` |
| UF, dólar, euro, UTM, IPC, TPM, IMACEC, cobre, desempleo, bitcoin | `indicadores_economicos` |
| Sismos recientes | `sismos` |
| Calidad del aire por región o comuna | `calidad_aire` |
| Farmacias de turno hoy | `farmacias_turno` |
| Licitaciones públicas | `licitaciones` |
| Órdenes de compra del Estado | `ordenes_compra` |
| RUT de proveedor → código de proveedor de Mercado Público | `proveedor_estado` |
| Nombre de organismo → código de organismo comprador | `organismos_publicos` |

## Flujos recomendados

### Datos abiertos (datos.gob.cl)

1. `catalogo` si es la primera vez en la conversación.
2. `categorias_datasets` para ubicar el slug del organismo (ej. `direccion_de_presupuestos`),
   o directo `buscar_datasets(q=...)` si el tema es claro.
3. `buscar_datasets(q?, organismo?)`: elegí un recurso **con `resource_id`**. Los marcados
   "sin DataStore" no se pueden consultar; solo se entrega su URL de descarga.
4. `ver_recurso(resource_id, muestra=3)`: leé los nombres exactos de los campos y sus tipos.
5. `consultar(resource_id, filtros=..., campos=..., ordenar=..., limite=...)` con los nombres
   de campo tal cual aparecen en el paso 4.

No te saltes el paso 4: los nombres de campo varían entre datasets (mayúsculas, tildes,
espacios) y un filtro con un nombre mal escrito falla o devuelve cero filas.

### Empresa o proveedor del Estado

1. `buscar_empresa(rut=...)`: constitución, tipo societario, capital, año del registro.
2. `proveedor_estado(rut=...)`: código de proveedor en Mercado Público.
3. `ordenes_compra(codigo_proveedor=..., fecha=...)` y/o `licitaciones(codigo_proveedor=...)`.
4. Con `codigo` en `ordenes_compra`/`licitaciones` se obtiene el detalle con montos.

### Compras de un organismo

1. `organismos_publicos(q="municipalidad de temuco")` → código del organismo.
2. `licitaciones(codigo_organismo=..., fecha=..., estado=...)` u `ordenes_compra(...)`.
3. Para expresar montos en UF o dólares: `indicadores_economicos(indicador="uf")`.

### Montos en otra unidad o en el tiempo

`indicadores_economicos(indicador=..., anio=AAAA)` devuelve la serie del año; sin `anio`,
el valor de hoy. Hacé la conversión vos mismo con los valores devueltos e indicá la fecha
de cada valor usado.

## Trampas conocidas

- **Filtros de `consultar` son texto exacto.** Los números se comparan como string:
  `{"Region":"13"}`, no `13` con otro formato. Acepta listas: `{"Region":["5","13"]}`.
- **`ordenar` es alfabético en campos text** ("10" < "9"). Revisá el tipo en `ver_recurso`;
  si es text, no confíes en un orden numérico ni en un "top N" por ese campo.
- **Registro de Empresas: 2014-2023 solo existen como CSV**, sin DataStore. `buscar_empresa`
  sin `anio` busca en los años consultables (2013 y 2024 en adelante). Que no aparezca una
  empresa no significa que no exista: el registro trae constituciones y modificaciones de
  cada año, no el universo de empresas de Chile.
- **Fechas de Mercado Público en `ddmmaaaa`** (ej. `06112026`), no ISO.
- **Mercado Público requiere `MERCADO_PUBLICO_TICKET`.** Si falta, la herramienta lo dice;
  explicale al usuario cómo pedirlo en lugar de reintentar.
- **No llames herramientas de Mercado Público en paralelo.** La API rechaza consultas
  simultáneas con el mismo ticket (código 10500). El servidor serializa y reintenta, pero
  en paralelo solo sumás espera.
- **Listados de Mercado Público son resumidos** (código, nombre, estado). Para montos,
  organismo y proveedor pedí el detalle con `codigo`.
- **Respuestas recortadas a ~20 KB.** Si la salida avisa que hay más, paginá con
  `offset`/`pagina` o reducí `campos` y `limite` en lugar de pedir todo.
- **`indicadores_economicos` puede venir de findic.cl** cuando mindicador.cl está caído. Es
  esperado; mencioná la fuente si el usuario pide precisión.
- **`farmacias_turno` es solo de hoy.** No sirve para preguntas de otros días.
- **`region` acepta número, romano o nombre**, y tildes/mayúsculas no importan en
  `calidad_aire` y `farmacias_turno`.

## Al responder

- Citá la fuente (datos.gob.cl, Mercado Público, mindicador/findic, CSN, SINCA, MINSAL) y la
  fecha u hora de los datos que devolvió la herramienta.
- Si un recurso no tiene DataStore, entregá la URL de descarga en vez de inventar valores.
- Si una fuente falla tras los reintentos, decilo y sugerí reintentar más tarde; no
  completes con datos de memoria.
