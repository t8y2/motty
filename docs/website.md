# The website

`website/` is a Next.js app (Fumadocs documentation plus a landing page) that
exports a **static** site to `website/out`. There is no server side: the search
index at `/api/search` is one prerendered file.

`.github/workflows/website.yml` builds `website/` and uploads `website/out` to
the Cloudflare Pages project `motty-page`, which serves <https://motty.page>.
A push to `main` that touches `website/**` deploys production; a pull request
only builds.

## One-time Cloudflare setup

The zone must already be active on Cloudflare (the registrar's nameservers point
at Cloudflare), so Pages can create the DNS records for the custom domains
itself.

1. **Create the Pages project**, once, as a *Direct Upload* project: the CI job
   uploads the built directory, Cloudflare does not build anything. Either

   ```sh
   cd website
   pnpm dlx wrangler@4 pages project create motty-page --production-branch main
   ```

   or *Workers & Pages → Create → Pages → Upload assets*, named `motty-page`.
   Do not connect the project to the git repository: that would build a second
   time on Cloudflare and race the workflow.

2. **Create an API token** at *My Profile → API Tokens → Create Token → Custom
   token*, with the single permission **Account → Cloudflare Pages → Edit**.
   Nothing else is needed; DNS and certificates are handled by the dashboard
   step below.

3. **Store the token and account id** as repository secrets. The account id is
   on the right-hand side of the dashboard, or from
   `pnpm dlx wrangler@4 whoami`.

   ```sh
   gh secret set CLOUDFLARE_API_TOKEN   # the token from step 2
   gh secret set CLOUDFLARE_ACCOUNT_ID  # e.g. 0123456789abcdef0123456789abcdef
   ```

4. **Attach the custom domains**: *project → Custom domains → Set up a custom
   domain* → `motty.page`, then again for `www.motty.page`. For a zone on
   Cloudflare the dashboard creates the proxied `CNAME motty-page.pages.dev`
   records and issues the certificate; no record has to be entered by hand.

   Attaching through the API does **not** create the records: the domain stays
   `pending` with "CNAME record not set" until the two records under *What a
   correct zone looks like* exist. Both names resolve, and `www` answers the
   same pages as the apex. That is only acceptable because step 5 redirects
   `www`.

5. **Redirect `www` to the apex**, so the canonical URL is `https://motty.page`:
   *Rules → Redirect Rules → Create rule*.

   - When incoming requests match:
     `http.host eq "www.motty.page" and not starts_with(http.request.uri.path, "/.well-known/acme-challenge/")`
   - Type: *Dynamic*, Expression:
     `concat("https://motty.page", http.request.uri.path)`
   - Status code: `301`, *Preserve query string* on.

   Redirect Rules run before the Pages project, so `www` requests never reach it
   and no duplicate content is served. The `/well-known/acme-challenge/`
   exception matters: the certificate Cloudflare issues for the `www` custom
   domain is validated over HTTP, and a rule that redirects that path leaves the
   domain stuck at *pending* forever.

6. **TLS**: *SSL/TLS → Overview* is *Full (strict)* for a Pages custom domain,
   and *Edge Certificates → Always Use HTTPS* should be on. Do not add a
   certificate by hand.

7. **Push** to `main` and watch the *Website* workflow. The first deploy needs
   the project from step 1 to exist; `wrangler pages deploy` fails with
   "project not found" otherwise.

## What a correct zone looks like

| Name | Type | Content | Proxy |
| --- | --- | --- | --- |
| `motty.page` | CNAME | `motty-page.pages.dev` | Proxied (flattened at the apex) |
| `www.motty.page` | CNAME | `motty-page.pages.dev` | Proxied |

The apex record is a `CNAME` at the root, which only works because Cloudflare
flattens it. A registrar that is not Cloudflare cannot hold that record; this is
one more reason the zone has to be on Cloudflare.

## Search engines and answer engines

Everything a crawler reads is generated from the same two sources: the metadata
blocks Next emits per page, and the content under `website/content/docs/`.

- `website/lib/seo.ts` holds the site constants, the per-language copy, the
  canonical/hreflang helper, the Open Graph and Twitter cards, the shared robots
  policy and the JSON-LD builders (Organization, WebSite, SoftwareApplication,
  FAQPage, BreadcrumbList, TechArticle).
- Each page sets its own canonical and hreflang set. `alternates()` only lists a
  translation that exists, and marks `x-default` as `/zh` because `/` answers 301
  to the Chinese home page.
- `/sitemap.xml`, `/robots.txt` and `/llms.txt` are static routes in `app/`; the
  sitemap walks `source.getPages()` for both languages, so adding an MDX file
  needs no sitemap edit. `llms.txt` lists the docs with their one-line
  descriptions for answer engines, and `robots.txt` names the AI crawlers
  explicitly so a future `Disallow` cannot silently block them.
- The `/` fallback page (public/_redirects sends it to `/zh`) and `/api/*` are
  `noindex`; `public/_headers` sets the types and short caches for the crawler
  files.
- `public/og.png` (1200×630) and the flattened `public/icon-512.png` /
  `public/apple-touch-icon.png` are committed assets. They were rendered from
  `public/logo-dark.svg` and `public/icon.svg` with `rsvg-convert`; redo them if
  the wordmark changes.

`defaultLanguage` in `lib/i18n.ts` follows the file naming, not the site's entry
point: unsuffixed `page.mdx` is the default language and `page.zh.mdx` is the
translation. It must stay `en`, or every `/en/docs` page falls back to the
Chinese body while keeping its English URL.

## Local checks

```sh
cd website
pnpm install --frozen-lockfile
pnpm build                    # writes out/ in about 15 seconds

# serve out/ with Pages' own clean-URL and 404 rules, not python -m http.server
pnpm dlx wrangler@4 pages dev out --port 8788
```

`out/` holds `index.html`, `en.html`, `zh.html`, the hashed bundles under
`_next/static/`, `404.html`, `sitemap.xml`, `robots.txt`, `llms.txt` and the
prerendered search index at `api/search`.
Pages serves `/en` from `en.html`; `python3 -m http.server` would return 404 for
it, so use `wrangler pages dev` when verifying routing.

## Rollback

*project → Deployments* keeps every upload; *⋯ → Rollback* on an earlier
deployment switches production back with no rebuild.

## Changing the domain or project name

The project name lives in `website/wrangler.toml` (`name`, and
`pages_build_output_dir` says what to upload); the workflow passes no project
name of its own. Rename the Pages project first, then update `wrangler.toml` and
the custom domains.
