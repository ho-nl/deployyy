# deployyy

Het public-facing deel van het [deployyy-platform](https://deployyy.app):
herbruikbare GitHub-Actions-build-workflows en platform-beheerde
Dockerfile-recepten per release-lijn. Een project-repo op het platform is
alleen nog code + een caller van ~6 regels — al het andere wordt afgeleid.

## Magento 2 bouwen

```yaml
# .github/workflows/preview-build.yaml in je project-repo
name: Preview build
on:
  push:
    branches-ignore: [main]
concurrency:
  group: preview-build-${{ github.ref_name }}
  cancel-in-progress: true
jobs:
  build:
    uses: ho-nl/deployyy/.github/workflows/magento2-build.yml@main
    secrets: inherit
```

De workflow:

1. **Leidt de release-lijn af** uit `composer.json` (nooit gedeclareerd):
   `mage-os/product-community-edition: 3.2.*` → `mageos-320`, enz.
2. **Bouwt met het platform-recept** van die lijn uit [`recipes/`](recipes/)
   — Dockerfile + `.platform/`-supportbestanden. Verbeteringen aan een
   recept bereiken elk consumerend repo bij z'n volgende build.
3. **Escape-hatch per niveau**: een repo-eigen `Dockerfile` wint wholesale;
   repo-eigen `.platform/<file>`-bestanden winnen per bestand.
4. **Per-project knoppen** staan in `deployyy.json`, bijv.
   `{"build": {"locales": ["nl_NL", "en_US"]}}` voor de
   static-content-locales (default `en_US`).
5. **Pusht commit-keyed tags** (`php-fpm-<sha7>` + `nginx-<sha7>`) naar
   `ghcr.io/<jouw-repo>` — de deployyy-operator verifieert die tags en
   deployt elke omgeving zelf. Niets deployt vanuit CI.

Eén secret op je eigen repo: `COMPOSER_AUTH` (de inhoud van je
`auth.json`; de legacy-naam `MAGENTO_AUTH_JSON` werkt ook). Bewust géén
org-secret — het platform werkt ook voor externe organisaties.

## Recepten

| Lijn | Dir | Status |
|---|---|---|
| `mageos-320` | [`recipes/mageos-320/`](recipes/mageos-320/) | ✅ live gevalideerd |

Een recept-rij bestaat pas na live validatie. Een lijn zonder recept faalt
de build met een duidelijke melding en de lijst van wat wél kan; je repo
kan dan tijdelijk een eigen Dockerfile shippen.

## Achtergrond

De operator zelf ([ho-nl/deployyy-operator](https://github.com/ho-nl/deployyy-operator))
beheert omgevingen als CRD's (branch = environment, previews met
scale-to-zero, migraties als state-machine). Dit repo is de publieke rand:
alles wat een project-repo — ook van derden — nodig heeft om op het
platform te bouwen.
