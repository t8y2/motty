# Additional application catalogs

These UTF-8 JSON catalogs contain every field of `i18n.Messages`, including its
nested sections. English and Simplified Chinese remain the source Go catalogs.

Export complete sources with:

```sh
go run ./tools/i18n-catalog -out source-catalogs.json
```

Keep Go printf directives, keyboard shortcuts, product names and code fragments
intact when translating. Run `go test ./internal/i18n` after a catalog change.
Language coverage, system tags and display names follow DBX's language list.
Shared UI terminology follows DBX's Apache-2.0 catalogs at commit
`c6533f00a9cd18718f1856ddbd04c7b92751068b`:
https://github.com/Ezreal-byte/dbx/tree/c6533f00a9cd18718f1856ddbd04c7b92751068b/apps/desktop/src/i18n
