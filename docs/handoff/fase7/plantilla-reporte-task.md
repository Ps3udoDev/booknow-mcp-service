# 7.X — <título de la tarea>

- **Fecha:** AAAA-MM-DD
- **Commit:** `<hash>` (o "pendiente de commit")
- **Estado:** hecha | parcial | bloqueada
- **Bloqueada por:** (decisión, migración, acceso… o "nada")

## Qué se implementó

Tres o cuatro frases en lenguaje llano: qué hace ahora el sistema que antes no hacía. Sin copiar el código.

## Archivos

| Archivo | Qué contiene |
|---|---|
| `internal/...` | … |

## Decisiones tomadas

- **<decisión>:** qué se eligió y por qué. Si contradice algo de `03-plan-fase7.md`, dilo explícitamente.

## Cómo se probó

- Tests añadidos (nombres y qué cubren).
- Comandos ejecutados y su resultado: `go test ./...`, `golangci-lint run`, tests de integración.
- Mutaciones probadas a mano y si los tests las detectaron:

| Mutación | ¿Detectada? |
|---|---|
| … | sí / no |

## Verificaciones en base de datos o servicios externos

Solo lecturas, sin PII ni secretos. Qué se consultó y qué salió.

## Lo que NO hace todavía

Límites conocidos, casos sin cubrir, deuda aceptada.

## Siguiente paso

Qué tarea sigue y qué necesita saber quien la tome (ficheros clave, contratos, gotchas).
