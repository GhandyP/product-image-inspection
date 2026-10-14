# product-image-inspection

Repo público de portafolio, `github.com/GhandyP/product-image-inspection`, construido desde cero en Go con
Google ADK (Go v2) y ODD. Evalúa una inspección de producto contra criterios declarados y produce un estado
validado por criterio con evidencia; la falta de evidencia o la ambigüedad se escalan a revisión humana.

Specs son el contrato verificable. Tasks son unidades de trabajo con un commit cada una. Log es append-only.

## Specs

S1. Repo nuevo, público, de portafolio, desde cero, en `github.com/GhandyP/product-image-inspection`.
   Cita del owner: "crea un repo publico que la idea es hacerlo desde el inicio".
   Cita del owner (nombre): `product-image-inspection`.

S2. Implementación en Go con Google ADK (Go v2) bajo ODD, con commits por unidad de trabajo.
   Citas del owner: "continua con crear en proyecto con odd", "implementa el plan con go google adk",
   "que sea un proyecto repo desde cero".

S3. Alcance de imagen (decisión del owner): "Notas + visión detrás de un puerto (fake en tests)". El camino de
   notas es determinista y offline; `internal/vision` expone un puerto `Describer` con un fake que responde
   desde fixtures y un adapter Gemini que manda la imagen real como parte multimodal. Go decide el estado por
   criterio; el modelo nunca decide pass/fail.
   Cita del owner: "Go decide el estado por criterio; el modelo nunca decide pass/fail."

S4. Entrega (decisión del owner): "Rama + PR y merge automático con CI verde". Bootstrap en `main`, trabajo en
   rama `feat/**`, PR con los commits por unidad de trabajo, merge cuando el CI pasa en verde.

S5. Cada criterio declarado recibe exactamente un estado validado, una evidencia y una confianza.
   Cita del brief del caso 37: "Each criterion has one validated status, evidence, and confidence."

S6. Evidencia visual ausente o ambigua se vuelve `review`, nunca una conjetura.
   Cita del brief del caso 37: "Missing or ambiguous visual evidence becomes `review`, not a guess."

S7. Fixtures de pass, fail, ocluido y baja luz, más observaciones contradictorias.
   Cita del brief del caso 37: "Fixtures cover pass/fail/occluded/low-light cases."
   Cita del brief del caso 37 (elementos faltantes): "Cover pass, fail, occluded, missing, low-light, and
   contradictory observations; test one result per criterion and confidence thresholds."

S8. Las decisiones de inventario requieren aprobación humana calificada: el programa nunca libera, rechaza ni
   reetiqueta inventario. El resultado declara esa aprobación como obligatoria.
   Cita del brief del caso 37: "Inventory decisions require qualified human approval."
   Cita del brief del caso 37: "do not release, reject, or relabel inventory automatically."

S9. Cada defecto exige una descripción de evidencia; una observación sin evidencia no produce veredicto.
   Cita de la guía del caso 37: "require an evidence description for every defect".

S10. Prueba rápida del caso: tres imágenes fixture — pass, fail y ambigua — y la ambigua debe escalarse.
   Cita de la guía del caso 37: "Three fixture images: pass, fail, and ambiguous; ambiguous must be escalated."

S11. La salida del modelo es entrada no confiable: toda observación debe citar textualmente la nota de
   inspección, no puede referirse a un criterio no declarado, y un payload con una sola violación se rechaza
   entero. Sin ese guard, el estado dejaría de estar respaldado por evidencia.

S12. El conjunto completo pasa sin `GOOGLE_API_KEY` y sin red: `gofmt`, `go vet`, `go build`, `go test -race`.
   Los caminos vivos (Gemini y visión real) se declaran no verificados en el README.

S13. ADK queda fijado en `google.golang.org/adk/v2 v2.2.0` con `go get` antes de `go mod tidy`, para que tidy
   no resuelva una versión más nueva ni arrastre otro toolchain.

S14. Códigos de salida estables y documentados: `0` todo criterio resuelto, `1` el run terminó y al menos un
   criterio necesita una persona, `2` uso, `3` entrada o payload rechazado, `4` modelo no disponible, `5`
   defecto del repositorio (un criterio sin chequeo o un resultado que rompe la garantía).

## Tasks

- T1 — Bootstrap: módulo, `.gitignore`, LICENSE MIT, workflow CI, copia del brief. Spec: S1, S2, S12, S13.
  Route: inline (degradación de autoridad Git conocida). Commit: `chore: scaffold the module, license, and repository metadata` (49d4552).
- T2 — `internal/inspection`: contrato (criterios, entrada, observación, estado, chequeo, resultado) y reglas
  estáticas que rechazan criterios duplicados o vacíos, IDs desconocidos, confianza fuera de rango y evidencia
  vacía. Spec: S5, S6, S8, S9. Route: inline. Commit: `feat(inspection): define the criteria, observation, and status contract` (a070006).
- T3 — `internal/evaluate`: puerta de evidencia determinista, un estado por criterio, umbral de confianza,
  contradicción y ambigüedad a `review`, aprobación humana obligatoria, y `Verify` como re-derivación
  independiente. Spec: S5, S6, S7, S9, S10. Route: inline.
  Commits: `feat(evaluate): derive one evidence-backed status per criterion` (73bc41e) y
  `refactor(inspection): validate observations against the evidence that was supplied` (a5138a3).
- T4 — `internal/agent`: puerto lector de notas, adapter ADK/Gemini con `OutputSchema` e `InstructionProvider`,
  guarda de cita textual, rechazo total del payload, fake offline. Spec: S3, S11, S12. Route: inline.
  Commit: `feat(agent): read notes into quoted observations the evaluator can trust` (f047d74).
- T5 — `internal/vision`: puerto `Describer`, fake desde fixtures, adapter Gemini multimodal que describe sin
  decidir, y `internal/llm` como único lugar que lee `ADK_MODEL`. Spec: S3, S6, S7, S10, S12. Route: inline.
  Commit: `feat(vision): describe an image behind a port the tests can replace` (36d4e1e).
- T6 — `internal/cli` y `main.go`: lectura de criterios y notas, JSON, códigos de salida y defecto interno.
  Spec: S5, S12, S14. Route: inline.
  Commit: `feat(cli): wire the inspection pipeline with documented exit codes` (eff2e4f).
- T7 — README (garantías, quick start, códigos de salida, límites, caminos no verificados) y fixtures completas
  del caso. Spec: S6, S7, S10, S12. Route: inline.
  Commits: `test: cover every case of the brief with the built command` (e9e95a7) y
  `docs: document the guarantees, quick start, limits, and exit codes` (2f4d917).
- T8 — Cierre: suite completa, assess nativo, push, PR, CI verde, merge y espejo en Engram. Spec: S2, S4, S12.
  Route: inline. Commit: `docs(odd): record the verification evidence and the close decision`.

## Log

L1 (pedido original del owner, verbatim):
"explora este proyecto , crea un repo publico que la idea es hacerlo desde el inicio pero para que sea un repo
dentro de mi portafolio en github , y luego continuar con crear en proyecto con odd , implementa el plan con go
google adk , que sea un proyecto repo desde cero"

L2 (decisiones del owner en la ronda de decisiones, verbatim de cada elección):
- Nombre del repo: `product-image-inspection`.
- Alcance imagen: "Notas + visión detrás de un puerto (fake en tests)".
- Entrega: "Rama + PR y merge automático con CI verde".

L3 (exploración, 2026-10-14): el directorio de la sesión es una copia separada del caso 37 del cookbook
(`~/Documents/P1_proyecticos/adk-go-enterprise-cookbook/casos/37-product-image-analysis`): un `README.md` con
brief y un `main.go` que importa `internal/adkhelper` del cookbook. Brief: shape `M + S`, salida
`{labels[], defects[], evidence_regions[]}`, "require an evidence description for every defect", y prueba
rápida de tres imágenes. Se preservan el brief y el prototipo en `odd/tasks/` y el repo se construye desde cero.

L4 (restricciones conocidas del entorno, de sesiones previas): `/home/ghandy` es un repositorio Git sin commits,
así que el proyecto es un repo anidado; `git init` degrada la autoridad Git de la sesión (subagentes y registro
de worktrees quedan inutilizables), por eso todas las tareas se rutean inline. `gentle_review assess` sigue
funcionando y es el árbitro del tier. Engram requiere `project` explícito.

L5 (arquitectura, 2026-10-14): el modelo queda fuera del camino de decisión. Su único trabajo es convertir notas
(o la descripción de una imagen) en observaciones estructuradas por criterio; `internal/evaluate` deriva el
estado. Un payload con una cita inventada, un criterio desconocido o confianza inválida se rechaza entero, y un
criterio sin chequeo es un defecto del repositorio (exit 5), no un resultado plausible.

L6 (precisión de S14, 2026-10-14): el código de salida `1` no existía en la primera escritura del spec y hacía
falta: `0` decía "todo criterio resuelto", pero un run con `review` es un resultado válido, no un error, y un
caller necesita distinguirlo para no tratar como fallo lo que es una respuesta. `1` es "el run terminó y al menos
un criterio necesita una persona". El resto de S14 queda igual.

L7 (T7, 2026-10-14): los fixtures del caso (pass, fail, ocluido, baja luz, evidencia faltante, contradictorio,
imágenes pass/fail/ambigua) se commitean en `testdata/` y `internal/cli/e2e_test.go` corre el binario construido
sobre todos ellos, porque los códigos de salida son una promesa al caller y `go run` los oculta.

L8 (evidencia de cierre, 2026-10-14). Verificación observada, no declarada:
- `gofmt -l .` sin salida; `go vet ./...`, `go build ./...` y `go test -race ./... -count=1` exit 0 sobre siete
  paquetes (inspection, evaluate, agent, vision, llm, cli y el main sin tests), sin `GOOGLE_API_KEY` y sin red.
- `internal/cli/e2e_test.go` construye el binario y lo corre sobre los diez fixtures del caso; los códigos 0/1/3/4
  y la evidencia impresa quedan asertados ahí, incluido que la imagen ambigua escala con
  `indeterminate_observation`.
- Assess nativo sobre el rango commiteado (`{"baseRef":"main","committedOnly":true}`): `risk: medium`,
  `nativeRisk: medium`, `reviewDue: false` (`under_budget`), `plan.independentVerifier: false`. La revisión nativa
  queda como vista externa adicional: no se inició un START porque el propio assess no lo pedía, y su outcome
  desconocido no bajó el listón — la verificación propia del tier es la que se ejecutó.
- CI: PR #1, dos runs de `Offline checks` en verde (38514686811 y 38514694067).

L9 (decisión de entrega, 2026-10-14): el owner había autorizado "Rama + PR y merge automático con CI verde", así
que el merge se hace con merge commit para conservar los ocho commits por unidad de trabajo en la historia. Los
caminos vivos de Gemini (`-notes` y `-image` sin `-observations`/`-descriptions`) quedan documentados y declarados
no verificados en el README: la suite corre offline a propósito.
