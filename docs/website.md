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

## Local checks

```sh
cd website
pnpm install --frozen-lockfile
pnpm build                    # writes out/ in about 15 seconds

# serve out/ with Pages' own clean-URL and 404 rules, not python -m http.server
pnpm dlx wrangler@4 pages dev out --port 8788
```

`out/` holds `index.html`, `en.html`, `zh.html`, the hashed bundles under
`_next/static/`, `404.html` and the prerendered search index at `api/search`.
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
