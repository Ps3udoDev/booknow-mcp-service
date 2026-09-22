# Runbook — despliegue en Cloud Run

Cómo desplegar, verificar y revertir `booknow-mcp-service` en Cloud Run (Fase 9 de `docs/roadmap.md`).
Los comandos con 🔒 los ejecuta el usuario con su cuenta de GCP; nunca se pegan secretos en el chat, en el repo ni en logs.

Archivos: `deploy/cloudrun/service.yaml` (manifiesto), `deploy/cloudrun/{staging,production}.env` (valores no secretos),
`deploy/cloudrun/render.sh` (genera el manifiesto; exige imagen por digest) y `deploy/cloudrun/smoke.sh` (smoke de solo lectura).

## Decisiones de despliegue

| Tema | Propuesta | Motivo |
|---|---|---|
| Región | `us-west1` (Oregón) | El Session Pooler de Supabase está en `aws-0-us-west-2` (Oregón): menos latencia por consulta. |
| Servicios | `booknow-mcp-staging` y `booknow-mcp` en el mismo proyecto, con secretos separados | Un solo proyecto basta para empezar; si se separan proyectos, solo cambian los `.env`. |
| Base de datos | Ambos contra Supabase de producción con el rol `booknow_mcp_service` | No hay otro proyecto Supabase. Las pruebas de escritura en staging, solo en el tenant `dev-test`. |
| Acceso en staging | Privado por IAM (`roles/run.invoker` solo para quien prueba) | Se prueba sin exponer el servicio. El token MCP va en `Authorization` y el de IAM en `X-Serverless-Authorization`. |
| Acceso en producción | Público; la autorización la hace la app (Bearer de Supabase + tenant) | Los clientes MCP no pueden presentar identidad de Google. |
| Conexiones | staging 1 instancia × 2 conexiones + producción 3 × 4 = **14** | En modo sesión, cada conexión del pool ocupa un cliente del pooler. Comprueba el **Pool Size** en Supabase → Database → Settings → Connection pooling y ajusta `MAX_INSTANCES`/`DB_MAX_CONNS` para que la suma quede por debajo. |
| Request | concurrencia 40, timeout 60 s, 1 vCPU, 256 MiB, CPU solo durante el request | `/mcp` responde JSON sin SSE y todo el trabajo (auditoría, `last_used_at`) ocurre dentro del request. |
| Probes | startup → `/readyz` (Postgres); liveness → `/healthz` (sin base) | Una caída breve de Supabase no debe reiniciar instancias sanas. |

## Preparación única 🔒

```bash
export PROJECT_ID=<gcp-project-id> REGION=us-west1
gcloud config set project "$PROJECT_ID"
gcloud services enable run.googleapis.com artifactregistry.googleapis.com secretmanager.googleapis.com \
  containerscanning.googleapis.com --quiet

# Registro de imágenes (con escaneo de vulnerabilidades al subir)
gcloud artifacts repositories create booknow --repository-format=docker --location="$REGION" --quiet
gcloud auth configure-docker "$REGION-docker.pkg.dev" --quiet

# Identidad del servicio: sin roles de proyecto; solo lee sus propios secretos
gcloud iam service-accounts create booknow-mcp-runtime --display-name="booknow-mcp runtime" --quiet
export RUNTIME_SA="booknow-mcp-runtime@$PROJECT_ID.iam.gserviceaccount.com"

# DATABASE_URL (Session Pooler, rol booknow_mcp_service). `read -s` evita que quede en el historial.
for secret in booknow-mcp-staging-database-url booknow-mcp-database-url; do
  read -rsp "DATABASE_URL para $secret: " DB_URL; echo
  printf '%s' "$DB_URL" | gcloud secrets create "$secret" --data-file=- --replication-policy=automatic --quiet
  gcloud secrets add-iam-policy-binding "$secret" \
    --member="serviceAccount:$RUNTIME_SA" --role=roles/secretmanager.secretAccessor --quiet
done
unset DB_URL
```

Después, completa los `<...>` de `deploy/cloudrun/staging.env` y `production.env` con `PROJECT_ID` y el número de proyecto
(`gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)'`). `render.sh` no genera nada mientras quede un `<...>`.

## Construir y publicar la imagen (por digest)

```bash
TAG="$REGION-docker.pkg.dev/$PROJECT_ID/booknow/booknow-mcp:$(git rev-parse --short HEAD)"
docker build --platform linux/amd64 -t "$TAG" .
docker push "$TAG"
IMAGE=$(docker inspect --format '{{index .RepoDigests 0}}' "$TAG")   # …/booknow-mcp@sha256:…
gcloud artifacts docker images describe "$IMAGE" --show-package-vulnerability   # revisar antes de desplegar
```

Staging y producción reciben **el mismo `$IMAGE`**: lo que pasó el smoke es exactamente lo que se promueve.

## Desplegar en staging

```bash
deploy/cloudrun/render.sh staging "$IMAGE" > /tmp/booknow-mcp-staging.yaml
gcloud run services replace /tmp/booknow-mcp-staging.yaml --region "$REGION"
# Staging es privado: solo tu cuenta puede invocarlo
gcloud run services add-iam-policy-binding booknow-mcp-staging --region "$REGION" \
  --member="user:$(gcloud config get-value account)" --role=roles/run.invoker --quiet
```

Smoke (sin token MCP comprueba transporte, probes, metadata OAuth, 401 y Origin; con token, también las tools):

```bash
URL=$(gcloud run services describe booknow-mcp-staging --region "$REGION" --format='value(status.url)')
IAM_TOKEN=$(gcloud auth print-identity-token) \
EXPECTED_RESOURCE="$(grep ^MCP_PUBLIC_URL= deploy/cloudrun/staging.env | cut -d= -f2)/mcp" \
SMOKE_ACCESS_TOKEN=<token OAuth de MCP Inspector> \
  deploy/cloudrun/smoke.sh "$URL"
```

`status.url` puede tener el formato antiguo (`…-<hash>-uw.a.run.app`). El recurso OAuth se publica con `MCP_PUBLIC_URL`,
por eso se pasa `EXPECTED_RESOURCE`. Los dos formatos sirven el mismo servicio.

Revisa también los logs: sin tokens, sin cadenas de conexión y sin teléfonos, y con la severidad correcta.

```bash
gcloud logging read 'resource.type="cloud_run_revision" AND resource.labels.service_name="booknow-mcp-staging"' \
  --limit=50 --format='value(severity,jsonPayload.message,jsonPayload.status)'
```

## Promover a producción

```bash
deploy/cloudrun/render.sh production "$IMAGE" > /tmp/booknow-mcp.yaml
gcloud run services replace /tmp/booknow-mcp.yaml --region "$REGION"
# Acceso público (la app exige Bearer + tenant en /mcp). Si una política de la organización bloquea allUsers,
# usa en su lugar: gcloud run services update booknow-mcp --region "$REGION" --no-invoker-iam-check
gcloud run services add-iam-policy-binding booknow-mcp --region "$REGION" \
  --member=allUsers --role=roles/run.invoker --quiet
deploy/cloudrun/smoke.sh "https://booknow-mcp-<project-number>.$REGION.run.app"
```

Los clientes MCP no se cambian a esta URL hasta la Fase 10 (QA y corte).

## Revertir

```bash
gcloud run revisions list --service booknow-mcp --region "$REGION"
gcloud run services update-traffic booknow-mcp --region "$REGION" --to-revisions=<revision-anterior>=100
deploy/cloudrun/smoke.sh <url>
```

El siguiente `services replace` vuelve a enviar el tráfico a la revisión nueva: despliega solo cuando esté corregido.

## Rotar `DATABASE_URL`

1. 🔒 Cambia la contraseña del rol en Supabase y añade una versión: `gcloud secrets versions add <secret> --data-file=-`.
2. Sube `DATABASE_URL_SECRET_VERSION` en el `.env` del entorno, vuelve a generar el manifiesto y aplícalo (crea una revisión nueva).
3. Smoke, y después `gcloud secrets versions disable <secret> --version=<anterior>`.

## Observabilidad y alertas

Los logs salen en JSON con `severity` y `message`, que Cloud Logging interpreta (`internal/platform/logging`). Alertas propuestas:

| Alerta | Fuente |
|---|---|
| 5xx | métrica `run.googleapis.com/request_count` con `response_code_class="5xx"` |
| Picos de 401 / 403 / 429 | la misma métrica con `response_code` |
| Latencia p95 | `run.googleapis.com/request_latencies` |
| JWKS no se refresca | métrica basada en logs: `jsonPayload.message="jwks refresh failed"` |
| Postgres no responde | métrica basada en logs: `jsonPayload.message="readiness check failed"` o `"mcp access resolution failed"` |
| Tool con error interno | métrica basada en logs: `jsonPayload.message="mcp tool failed"` |

## Pendiente (no bloquea el primer staging)

- Pipeline de despliegue (GitHub Actions con Workload Identity Federation) con SBOM y provenance. Hasta entonces se despliega a mano con este runbook.
- Dominio propio con HTTPS: cuando exista, cambiar `MCP_PUBLIC_URL` en `production.env`.
