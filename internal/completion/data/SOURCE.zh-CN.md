# Simplified Chinese command descriptions

The English keys in `internal/workbench/completion_locale.go` come from the
bundled [Carapace catalog](SOURCE.txt), licensed under [MIT](LICENSE.carapace).
The Chinese wording was selected and edited with AI assistance from an earlier
offline draft made with Argos Translate 1.9.0 and its English-to-Chinese
OPUS-MT package v1.9. The package credits Jörg Tiedemann and Santhosh
Thottingal and identifies the original OPUS model licence as
[CC BY 4.0](https://creativecommons.org/licenses/by/4.0/).

For Git terminology, compare the [Git project's Simplified Chinese
catalog](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po).
The PO header specifies the same licence as Git (see Git's [GPLv2
notice](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/COPYING)).
The Git translations serve as terminology references. The four existing
Chinese descriptions in this batch predate the comparison, including the
short phrase "显示提交日志"; `git status` has independently edited wording
using "工作区". This Apache-2.0 project does not incorporate the Git PO
translation dataset.

The bundled Carapace Git entries match the Git PO English keys for
[`clone`](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po#L20056),
[`fetch`](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po#L20152),
and [`log`](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po#L20256).
The [`status`](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po#L20506)
key differs only in initial capitalization. Carapace's `diff` description
is limited to commits, whereas [Git's PO entry](https://github.com/git/git/blob/acba8bf9753134d9bde89f94369f775838a8c5fe/po/zh_CN.po#L20120)
also describes comparisons with the working tree.
Review additions against every matching command and alias in the bundled
catalog. Ask a Chinese-speaking reviewer to check the wording in context
before merging a translation batch.
