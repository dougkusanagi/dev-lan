# Fixtures de contrato do proxy

As fixtures são intencionalmente pequenas e não executam dependências de
projeto. Elas cobrem os contratos que precisam funcionar nas duas origens:

| Fixture | Contratos |
| --- | --- |
| `php` | raiz, asset absoluto, redirect, cookie e origem |
| `static` | arquivo estático, asset absoluto e fallback de SPA |
| `vite` | asset absoluto, HMR/WebSocket e origem |
| `ssr` | resposta SSR, redirect, cookie, origem e WebSocket |

`manifest.json` é a lista executável desses checks. O servidor `ssr/server.mjs`
usa somente APIs nativas do Node, portanto o smoke não precisa instalar
dependências.
