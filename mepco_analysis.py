#!/usr/bin/env python3
"""
Análisis de Sostenibilidad del MEPCO - Chile
Monte Carlo Simulation + VaR + Riesgo Fiscal
Fecha de datos: 2 de abril de 2026
"""

import numpy as np
from dataclasses import dataclass

np.random.seed(42)

# ============================================================
# PASO 1: PARÁMETROS DE MERCADO (datos reales al 2 abril 2026)
# ============================================================

@dataclass
class MarketData:
    # Brent Crude Oil
    brent_spot: float = 109.0          # USD/bbl (2 abril 2026)
    brent_peak_march: float = 126.0    # Pico marzo 2026
    brent_pre_crisis: float = 75.0     # Pre-cierre Ormuz (feb 2026)

    # Curva de futuros (backwardation pronunciada)
    brent_1m: float = 108.0            # Mayo 2026
    brent_3m: float = 95.0             # Julio 2026 (EIA forecast: >$95 próx 2 meses)
    brent_6m: float = 82.0             # Octubre 2026 (EIA: ~$80 en Q3)
    brent_12m: float = 74.0            # Abril 2027 (futuros Dec26: $77.88)

    # Tipo de cambio
    usdclp_spot: float = 915.95        # Dólar observado BCCh 2 abril 2026
    usdclp_pre_crisis: float = 867.0   # Pre-crisis feb 2026
    usdclp_3m_fwd: float = 930.0       # Estimado (diferencial tasas ~2%)
    usdclp_6m_fwd: float = 948.0       # Estimado

    # Consumo Chile
    consumo_bbl_dia: float = 386_000   # Barriles/día (2024, Worldometers)
    pct_importado: float = 0.95        # 95% importado

    # MEPCO
    mepco_costo_semanal_usd: float = 170e6    # ~USD 170M/semana (rango 140-200M)
    mepco_subsidio_gasolina_clp_l: float = 270 # CLP/litro
    mepco_subsidio_diesel_clp_l: float = 470   # CLP/litro
    brent_referencia_mepco: float = 75.0       # Precio donde MEPCO es ~neutral

    # FEES
    fees_saldo_usd: float = 3_944e6            # USD 3,944M (feb 2026)

    # Volatilidad
    # CBOE OVX rango 52 semanas: 23.59 - 125.99
    # Brent subió 42% solo en marzo 2026
    # Uso 75% anualizada como estimación conservadora del régimen actual
    vol_brent_anual: float = 0.75              # 75% anualizada
    vol_usdclp_anual: float = 0.15             # 15% anualizada
    corr_brent_clp: float = 0.45               # Correlación positiva (petróleo sube → CLP se debilita)

mkt = MarketData()

# ============================================================
# PASO 2: SIMULACIÓN MONTE CARLO (10,000 escenarios)
# ============================================================

N_SIM = 10_000
TRADING_DAYS_YEAR = 252
horizons = {
    "3 meses": 63,    # ~63 días hábiles
    "6 meses": 126,
    "12 meses": 252,
}

def annualized_drift(spot, future, days):
    """Calcula drift anualizado implícito en la curva de futuros."""
    if days == 0:
        return 0.0
    t = days / TRADING_DAYS_YEAR
    return np.log(future / spot) / t

# Drifts implícitos de la curva de futuros (backwardation → drift negativo)
drift_brent_3m = annualized_drift(mkt.brent_spot, mkt.brent_3m, 63)
drift_brent_6m = annualized_drift(mkt.brent_spot, mkt.brent_6m, 126)
drift_brent_12m = annualized_drift(mkt.brent_spot, mkt.brent_12m, 252)

# Drift para USD/CLP (basado en diferencial de forwards)
drift_clp_3m = annualized_drift(mkt.usdclp_spot, mkt.usdclp_3m_fwd, 63)
drift_clp_6m = annualized_drift(mkt.usdclp_spot, mkt.usdclp_6m_fwd, 126)

print("=" * 70)
print("ANÁLISIS DE SOSTENIBILIDAD DEL MEPCO - CHILE")
print("Fecha de análisis: 2 de abril de 2026")
print("=" * 70)

print("\n📊 DATOS DE MERCADO")
print("-" * 50)
print(f"Brent spot:              USD {mkt.brent_spot:.2f}/bbl")
print(f"Brent futuro 3m:         USD {mkt.brent_3m:.2f}/bbl")
print(f"Brent futuro 6m:         USD {mkt.brent_6m:.2f}/bbl")
print(f"Brent futuro 12m:        USD {mkt.brent_12m:.2f}/bbl")
print(f"Drift implícito 3m:      {drift_brent_3m*100:.1f}% anual")
print(f"Drift implícito 6m:      {drift_brent_6m*100:.1f}% anual")
print(f"Drift implícito 12m:     {drift_brent_12m*100:.1f}% anual")
print(f"Estructura:              BACKWARDATION PRONUNCIADA")
print(f"USD/CLP spot:            {mkt.usdclp_spot:.2f}")
print(f"Vol Brent (anualizada):  {mkt.vol_brent_anual*100:.0f}%")
print(f"Vol USD/CLP (anual):     {mkt.vol_usdclp_anual*100:.0f}%")
print(f"Correlación Brent-CLP:   {mkt.corr_brent_clp:.2f}")
print(f"FEES disponible:         USD {mkt.fees_saldo_usd/1e9:.2f} bn")
print(f"MEPCO costo semanal:     USD {mkt.mepco_costo_semanal_usd/1e6:.0f}M")

# Generar shocks correlacionados
def simulate_correlated_paths(n_days, drift_brent, drift_clp, n_sim=N_SIM):
    """Simula paths correlacionados para Brent y USD/CLP con GBM."""
    dt = 1 / TRADING_DAYS_YEAR

    # Matriz de correlación
    corr_matrix = np.array([[1.0, mkt.corr_brent_clp],
                             [mkt.corr_brent_clp, 1.0]])
    L = np.linalg.cholesky(corr_matrix)

    # Generar shocks independientes y correlacionarlos
    z_independent = np.random.standard_normal((n_sim, n_days, 2))
    z_correlated = np.einsum('ijk,lk->ijl', z_independent, L)

    # Paths del Brent (GBM)
    brent_paths = np.zeros((n_sim, n_days + 1))
    brent_paths[:, 0] = mkt.brent_spot

    clp_paths = np.zeros((n_sim, n_days + 1))
    clp_paths[:, 0] = mkt.usdclp_spot

    for t in range(n_days):
        # Brent
        brent_paths[:, t+1] = brent_paths[:, t] * np.exp(
            (drift_brent - 0.5 * mkt.vol_brent_anual**2) * dt +
            mkt.vol_brent_anual * np.sqrt(dt) * z_correlated[:, t, 0]
        )
        # USD/CLP
        clp_paths[:, t+1] = clp_paths[:, t] * np.exp(
            (drift_clp - 0.5 * mkt.vol_usdclp_anual**2) * dt +
            mkt.vol_usdclp_anual * np.sqrt(dt) * z_correlated[:, t, 1]
        )

    return brent_paths, clp_paths

def calculate_mepco_cost(brent_price, fx_rate):
    """
    Calcula costo semanal del MEPCO en USD.
    Modelo: proporcional a (Brent - referencia) * efecto_tipo_cambio
    Calibrado: a Brent=$109, FX=916 → costo = USD 170M/semana
    Si Brent < referencia, MEPCO recauda (costo negativo).
    """
    gap = brent_price - mkt.brent_referencia_mepco
    fx_factor = fx_rate / mkt.usdclp_spot
    base_cost = mkt.mepco_costo_semanal_usd
    base_gap = mkt.brent_spot - mkt.brent_referencia_mepco  # $34

    cost = base_cost * (gap / base_gap) * fx_factor
    return max(cost, 0)  # MEPCO no "cobra" más allá de recaudación normal

# ============================================================
# SIMULACIÓN PARA CADA HORIZONTE
# ============================================================

results = {}

for horizon_name, n_days in horizons.items():
    if "3" in horizon_name:
        d_brent, d_clp = drift_brent_3m, drift_clp_3m
    elif "6" in horizon_name:
        d_brent, d_clp = drift_brent_6m, drift_clp_6m
    else:
        d_brent, d_clp = drift_brent_12m, drift_clp_6m * 0.8  # decay del drift CLP

    brent_paths, clp_paths = simulate_correlated_paths(n_days, d_brent, d_clp)

    # Precio final del Brent
    brent_final = brent_paths[:, -1]
    clp_final = clp_paths[:, -1]

    # Calcular costo acumulado del MEPCO
    n_weeks = n_days / 5  # Semanas hábiles
    weekly_costs = np.zeros(N_SIM)

    # Usar precio promedio del path para estimar costo promedio semanal
    brent_avg = np.mean(brent_paths[:, 1:], axis=1)
    clp_avg = np.mean(clp_paths[:, 1:], axis=1)

    for i in range(N_SIM):
        weekly_cost = calculate_mepco_cost(brent_avg[i], clp_avg[i])
        weekly_costs[i] = weekly_cost

    cumulative_cost = weekly_costs * n_weeks

    results[horizon_name] = {
        "brent_final": brent_final,
        "clp_final": clp_final,
        "brent_avg": brent_avg,
        "clp_avg": clp_avg,
        "weekly_cost_avg": weekly_costs,
        "cumulative_cost": cumulative_cost,
        "n_weeks": n_weeks,
    }

# ============================================================
# PASO 3: RESULTADOS
# ============================================================

print("\n" + "=" * 70)
print("RESULTADOS DE LA SIMULACIÓN MONTE CARLO (10,000 escenarios)")
print("=" * 70)

for horizon_name in horizons:
    r = results[horizon_name]
    print(f"\n{'─' * 60}")
    print(f"  HORIZONTE: {horizon_name.upper()}")
    print(f"{'─' * 60}")

    bf = r["brent_final"]
    cf = r["clp_final"]
    cc = r["cumulative_cost"]

    print(f"\n  Brent al final del período:")
    print(f"    Media:         USD {np.mean(bf):.2f}/bbl")
    print(f"    Mediana:       USD {np.median(bf):.2f}/bbl")
    print(f"    Percentil 5:   USD {np.percentile(bf, 5):.2f}/bbl")
    print(f"    Percentil 25:  USD {np.percentile(bf, 25):.2f}/bbl")
    print(f"    Percentil 75:  USD {np.percentile(bf, 75):.2f}/bbl")
    print(f"    Percentil 95:  USD {np.percentile(bf, 95):.2f}/bbl")
    print(f"    Percentil 99:  USD {np.percentile(bf, 99):.2f}/bbl")

    print(f"\n  USD/CLP al final del período:")
    print(f"    Media:         {np.mean(cf):.0f}")
    print(f"    Percentil 5:   {np.percentile(cf, 5):.0f}")
    print(f"    Percentil 95:  {np.percentile(cf, 95):.0f}")

    print(f"\n  Costo acumulado del MEPCO:")
    print(f"    Media:         USD {np.mean(cc)/1e9:.2f} bn")
    print(f"    Mediana:       USD {np.median(cc)/1e9:.2f} bn")
    print(f"    Percentil 75:  USD {np.percentile(cc, 75)/1e9:.2f} bn")
    print(f"    Percentil 95:  USD {np.percentile(cc, 95)/1e9:.2f} bn (VaR 95%)")
    print(f"    Percentil 99:  USD {np.percentile(cc, 99)/1e9:.2f} bn (VaR 99%)")
    print(f"    Máximo:        USD {np.max(cc)/1e9:.2f} bn")

# ============================================================
# ANÁLISIS DE SOSTENIBILIDAD
# ============================================================

print("\n" + "=" * 70)
print("ANÁLISIS DE SOSTENIBILIDAD FISCAL")
print("=" * 70)

fees = mkt.fees_saldo_usd

# Pregunta 1: Probabilidad de superar FEES en 6 meses
cc_6m = results["6 meses"]["cumulative_cost"]
prob_exceed_fees_6m = np.mean(cc_6m > fees) * 100
print(f"\n1. Probabilidad de que el costo acumulado del MEPCO supere")
print(f"   el FEES (USD {fees/1e9:.2f} bn) en 6 meses: {prob_exceed_fees_6m:.1f}%")

cc_3m = results["3 meses"]["cumulative_cost"]
prob_exceed_fees_3m = np.mean(cc_3m > fees) * 100
print(f"   En 3 meses: {prob_exceed_fees_3m:.1f}%")

cc_12m = results["12 meses"]["cumulative_cost"]
prob_exceed_fees_12m = np.mean(cc_12m > fees) * 100
print(f"   En 12 meses: {prob_exceed_fees_12m:.1f}%")

# Pregunta 2: Costo esperado
print(f"\n2. Costo esperado (media) del MEPCO:")
print(f"   3 meses:  USD {np.mean(cc_3m)/1e9:.2f} bn")
print(f"   6 meses:  USD {np.mean(cc_6m)/1e9:.2f} bn")
print(f"   12 meses: USD {np.mean(cc_12m)/1e9:.2f} bn")

# Pregunta 3: Peor escenario
print(f"\n3. Peor escenario (percentil 99):")
print(f"   3 meses:  USD {np.percentile(cc_3m, 99)/1e9:.2f} bn")
print(f"   6 meses:  USD {np.percentile(cc_6m, 99)/1e9:.2f} bn")
print(f"   12 meses: USD {np.percentile(cc_12m, 99)/1e9:.2f} bn")

# Pregunta 4: Asimetría del riesgo
print(f"\n4. Asimetría del riesgo (6 meses):")
mean_6m = np.mean(cc_6m)
upside = mean_6m - np.percentile(cc_6m, 5)    # "Ahorro" en escenario bueno
downside = np.percentile(cc_6m, 95) - mean_6m  # "Costo extra" en escenario malo
print(f"   Costo medio:              USD {mean_6m/1e9:.2f} bn")
print(f"   Ahorro vs escenario bueno (P5):  USD {upside/1e9:.2f} bn")
print(f"   Costo extra vs esc. malo (P95):  USD {downside/1e9:.2f} bn")
print(f"   Ratio asimetría (downside/upside): {downside/upside:.2f}x")
if downside > upside:
    print(f"   → RIESGO ASIMÉTRICO: el escenario malo cuesta {downside/upside:.1f}x más")
    print(f"     de lo que el escenario bueno ahorra")
else:
    print(f"   → Riesgo relativamente simétrico")

# Pregunta 5: Precio de quiebre
print(f"\n5. Precio del Brent de insostenibilidad fiscal:")
# A qué precio promedio el costo de 12 meses supera el FEES
target_cost = fees
weeks_12m = 52
# cost = base_cost * (brent - ref) / base_gap * fx_factor * weeks
# Resolver para brent:
# target = 170e6 * (brent - 75) / 34 * (fx/916) * 52
# Asumiendo FX = 930 (promedio forward)
fx_avg = 930
breakeven_brent = (target_cost * 34 / (170e6 * (fx_avg/916) * weeks_12m)) + 75
print(f"   Si Brent promedio se mantiene sobre USD {breakeven_brent:.0f}/bbl por 12 meses,")
print(f"   el MEPCO agota el FEES completo (USD {fees/1e9:.2f} bn)")

# Para 6 meses
weeks_6m = 26
breakeven_brent_6m = (target_cost * 34 / (170e6 * (fx_avg/916) * weeks_6m)) + 75
print(f"   Para agotar FEES en 6 meses: Brent promedio > USD {breakeven_brent_6m:.0f}/bbl")

# Pregunta 6: Señal de la curva de futuros
print(f"\n6. Señal de la curva de futuros:")
print(f"   Spot:    USD {mkt.brent_spot:.0f}/bbl")
print(f"   3 meses: USD {mkt.brent_3m:.0f}/bbl ({((mkt.brent_3m/mkt.brent_spot)-1)*100:.1f}%)")
print(f"   6 meses: USD {mkt.brent_6m:.0f}/bbl ({((mkt.brent_6m/mkt.brent_spot)-1)*100:.1f}%)")
print(f"   12 meses:USD {mkt.brent_12m:.0f}/bbl ({((mkt.brent_12m/mkt.brent_spot)-1)*100:.1f}%)")
print(f"   → BACKWARDATION PRONUNCIADA: el mercado anticipa que los precios")
print(f"     BAJARÁN significativamente. Futuros a 12m están 32% bajo spot.")
print(f"     Esto sugiere que el mercado ve la crisis de Ormuz como TRANSITORIA.")

# ============================================================
# PASO 4: TABLA RESUMEN DE ESCENARIOS
# ============================================================

print("\n" + "=" * 70)
print("TABLA RESUMEN DE ESCENARIOS")
print("=" * 70)

scenarios = [
    ("OPTIMISTA (P10)", 10),
    ("BASE (P50/Mediana)", 50),
    ("PESIMISTA (P90)", 90),
    ("CATASTRÓFICO (P99)", 99),
]

for period_name in ["3 meses", "6 meses", "12 meses"]:
    r = results[period_name]
    print(f"\n{'─' * 60}")
    print(f"  {period_name.upper()}")
    print(f"{'─' * 60}")
    print(f"  {'Escenario':<22} {'Brent':>10} {'USD/CLP':>10} {'Costo MEPCO':>15} {'% FEES':>8}")
    print(f"  {'':─<22} {'':─>10} {'':─>10} {'':─>15} {'':─>8}")

    for name, pctl in scenarios:
        brent_val = np.percentile(r["brent_final"], pctl)
        clp_val = np.percentile(r["clp_final"], pctl)
        cost_val = np.percentile(r["cumulative_cost"], pctl)
        fees_pct = cost_val / fees * 100
        print(f"  {name:<22} ${brent_val:>8.0f} {clp_val:>10.0f} USD {cost_val/1e9:>7.2f} bn {fees_pct:>7.0f}%")

# ============================================================
# ANÁLISIS DE SENSIBILIDAD
# ============================================================

print("\n" + "=" * 70)
print("ANÁLISIS DE SENSIBILIDAD")
print("=" * 70)

sensitivity_prices = [80, 90, 100, 109, 120, 130, 150]
fx_scenarios = [880, 916, 950, 1000]

print(f"\n  Costo semanal del MEPCO (USD millones) según Brent y USD/CLP:")
print(f"\n  {'Brent':>8}", end="")
for fx in fx_scenarios:
    print(f"  FX={fx:>4}", end="")
print()
print(f"  {'':─>8}", end="")
for fx in fx_scenarios:
    print(f"  {'':─>8}", end="")
print()

for brent in sensitivity_prices:
    print(f"  ${brent:>6}", end="")
    for fx in fx_scenarios:
        cost = calculate_mepco_cost(brent, fx) / 1e6
        print(f"  ${cost:>6.0f}M", end="")
    print()

print(f"\n  Costo acumulado 6 meses (USD bn) si precio se mantiene constante:")
print(f"\n  {'Brent':>8}", end="")
for fx in fx_scenarios:
    print(f"  FX={fx:>4}", end="")
print()
print(f"  {'':─>8}", end="")
for fx in fx_scenarios:
    print(f"  {'':─>8}", end="")
print()

for brent in sensitivity_prices:
    print(f"  ${brent:>6}", end="")
    for fx in fx_scenarios:
        cost_6m = calculate_mepco_cost(brent, fx) * 26 / 1e9  # 26 semanas
        print(f"  ${cost_6m:>5.1f}bn", end="")
    print()

# ============================================================
# CONCLUSIÓN
# ============================================================

print("\n" + "=" * 70)
print("CONCLUSIÓN")
print("=" * 70)

mean_cost_6m = np.mean(cc_6m) / 1e9
mean_cost_12m = np.mean(cc_12m) / 1e9
var95_6m = np.percentile(cc_6m, 95) / 1e9
var99_6m = np.percentile(cc_6m, 99) / 1e9

print(f"""
VEREDICTO: El MEPCO es FISCALMENTE INSOSTENIBLE en su forma actual.

Datos clave:
• FEES disponible: USD {fees/1e9:.2f} bn
• Costo esperado 6 meses: USD {mean_cost_6m:.2f} bn ({mean_cost_6m/fees*1e9*100:.0f}% del FEES)
• Costo esperado 12 meses: USD {mean_cost_12m:.2f} bn ({mean_cost_12m/fees*1e9*100:.0f}% del FEES)
• VaR 95% a 6 meses: USD {var95_6m:.2f} bn
• VaR 99% a 6 meses: USD {var99_6m:.2f} bn

Horizontes de quiebre:
• Si Brent se mantiene ~$109: FEES se agota en ~{fees/(170e6*52)*12:.0f} meses
• Si Brent baja a $95 (futuros 3m): costo baja a ~$100M/semana → FEES dura ~{fees/(100e6*52)*12:.0f} meses
• Si Brent baja a $80: costo baja a ~$25M/semana → FEES dura ~{fees/(25e6*52)*12:.0f} meses (manejable)
• Si Brent sube a $130: costo sube a ~$275M/semana → FEES se agota en ~{fees/(275e6*52)*12:.0f} meses

Probabilidades:
• Prob. de agotar FEES en 6 meses: {prob_exceed_fees_6m:.1f}%
• Prob. de agotar FEES en 12 meses: {prob_exceed_fees_12m:.1f}%

Riesgo asimétrico:
• El escenario malo (P95) cuesta {downside/upside:.1f}x más de lo que el bueno ahorra
• La correlación Brent↑ = CLP↓ amplifica las pérdidas (doble golpe)

El mercado de futuros dice:
• Backwardation pronunciada (futuros 12m en $74 vs spot $109)
• El mercado espera que la crisis de Ormuz sea TRANSITORIA
• Si se resuelve → precios vuelven a ~$75-80 → MEPCO se estabiliza
• Si NO se resuelve en 3-6 meses → quiebre fiscal inevitable

RECOMENDACIÓN:
El gobierno de Kast hizo bien en ajustar el MEPCO (2→4 semanas, alzas de $370-580/L).
Pero si Ormuz no se reabre en Q3 2026, necesitará:
1. Nuevos ajustes al alza del MEPCO (traspasar más costo al consumidor)
2. Recurrir al FEES de forma controlada
3. Evaluar un tope máximo de subsidio semanal
4. Preparar financiamiento contingente (deuda) si el escenario catastrófico se materializa
""")

# Distribución de probabilidades del costo acumulado a 6 meses
print("=" * 70)
print("DISTRIBUCIÓN DE PROBABILIDAD - COSTO ACUMULADO MEPCO 6 MESES")
print("=" * 70)

bins_usd = [0, 1e9, 2e9, 3e9, 4e9, 5e9, 7e9, 10e9, 15e9, 20e9]
labels = ["<$1bn", "$1-2bn", "$2-3bn", "$3-4bn", "$4-5bn", "$5-7bn", "$7-10bn", "$10-15bn", ">$15bn"]

print(f"\n  {'Rango costo':>15} {'Probabilidad':>12} {'Barra':>3}")
print(f"  {'':─>15} {'':─>12} {'':─>3}")

for i in range(len(bins_usd) - 1):
    count = np.sum((cc_6m >= bins_usd[i]) & (cc_6m < bins_usd[i+1]))
    prob = count / N_SIM * 100
    bar = "█" * int(prob / 2)
    print(f"  {labels[i]:>15} {prob:>10.1f}%  {bar}")

count_above = np.sum(cc_6m >= bins_usd[-1])
prob_above = count_above / N_SIM * 100
bar = "█" * int(prob_above / 2)
print(f"  {labels[-1]:>15} {prob_above:>10.1f}%  {bar}")

# Línea del FEES
print(f"\n  ─── FEES (USD {fees/1e9:.1f} bn) ───")
print(f"  Probabilidad de estar BAJO el FEES: {(1-prob_exceed_fees_6m/100)*100:.1f}%")
print(f"  Probabilidad de SUPERAR el FEES:    {prob_exceed_fees_6m:.1f}%")

print("\n" + "=" * 70)
print("FIN DEL ANÁLISIS")
print("=" * 70)
