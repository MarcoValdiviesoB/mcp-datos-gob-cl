package text

import (
	"fmt"
	"strconv"
	"strings"
)

// NormRUT deja un RUT como "12345678-K": sin puntos ni espacios, con guion y DV en mayúscula.
// Valida el dígito verificador.
func NormRUT(s string) (string, error) {
	s = strings.ToUpper(strings.NewReplacer(".", "", " ", "", "-", "", "‐", "").Replace(strings.TrimSpace(s)))
	if len(s) < 2 {
		return "", fmt.Errorf("RUT vacío o muy corto; usá el formato 12.345.678-9")
	}
	body, dv := s[:len(s)-1], s[len(s)-1:]
	body = strings.TrimLeft(body, "0")
	if body == "" || len(body) > 9 {
		return "", fmt.Errorf("RUT %q inválido; usá el formato 12.345.678-9", s)
	}
	for _, c := range body {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("RUT %q inválido: el cuerpo debe ser numérico (formato 12.345.678-9)", s)
		}
	}
	if want := RutDV(body); want != dv {
		return "", fmt.Errorf("RUT %s-%s con dígito verificador incorrecto (para %s debería ser %s)", body, dv, body, want)
	}
	return body + "-" + dv, nil
}

// RutDV calcula el dígito verificador (módulo 11) de un cuerpo numérico.
func RutDV(body string) string {
	sum, mul := 0, 2
	for i := len(body) - 1; i >= 0; i-- {
		sum += int(body[i]-'0') * mul
		mul++
		if mul > 7 {
			mul = 2
		}
	}
	switch r := 11 - sum%11; r {
	case 11:
		return "0"
	case 10:
		return "K"
	default:
		return strconv.Itoa(r)
	}
}

// RutConPuntos formatea un RUT normalizado como lo espera Mercado Público: "70.017.820-k".
func RutConPuntos(rut string) string {
	body, dv, _ := strings.Cut(rut, "-")
	var out []byte
	for i := range body {
		if i > 0 && (len(body)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, body[i])
	}
	return string(out) + "-" + strings.ToLower(dv)
}
