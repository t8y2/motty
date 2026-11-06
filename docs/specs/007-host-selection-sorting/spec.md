# Host selection and display ordering

Version: 1.0, 2026-11-06. Approved scope: [Issue #507 proposal](https://github.com/t8y2/motty/issues/507#issuecomment-6679323401), the requester's [IP sorting addition](https://github.com/t8y2/motty/issues/507#issuecomment-6679684994), and the user's explicit authorization to implement, validate headlessly and submit a PR. Baseline: `b96f514` on upstream `main`; the issue is claimed by `caichangqing1120`.

## Behavior

- Add an always-available three-state select-all checkbox in the hosts toolbar, and a three-state checkbox on each group row in both list and card views. Empty scopes are disabled. Space activates a focused selection control.
- Selection scope is the current search and group filter, including matching hosts inside collapsed groups and outside the scrolled viewport. Group selection further restricts this scope to the group and all descendants; the unnamed group selects only ungrouped hosts. Partial selection becomes full; full selection becomes empty. Selection outside the operated scope is preserved, following existing search-selection behavior. Changing a group filter continues clearing selection.
- Batch selection uses the existing host selection IDs and export path. Exported host catalog schema, connection fields, secrets policy and group relationships remain unchanged.
- Offer Manual, Name and IP address display orders. Manual is the default and preserves the upstream host drag ordering, group pin/order and original host sequence. Automatic ordering never rewrites these manual orders or host identities.
- Name order sorts sibling groups and hosts within each group by lowercase name, original name, then stable catalog ID for hosts. Group subtrees remain contiguous. IP order sorts groups by name and hosts within a group by numeric IPv4 then IPv6, followed by non-IP addresses in case-insensitive string order. IPv4-mapped IPv6 addresses compare as IPv4. Equal addresses use name and stable catalog ID. No DNS queries are performed.
- Persist the selected order as an optional shared scalar setting, using existing record synchronization. Unset or unknown values display Manual. WebDAV synchronization propagates the order preference and existing manual host/group ordering, while fold state remains device-local.
- Keep the existing manual-reorder mode, but require Manual display order for host drag handles and group up/down actions. Choosing an automatic order cancels an active reorder gesture. Existing group relocation remains available.
- Reuse Viva components, theme, compiled icons and all three current locales (English, simplified Chinese, traditional Chinese). Keep warm hosts painting allocation-free, with sorting and selection aggregation outside Paint.

## Acceptance

1. Select-all, partial selection, clearing and group subtree selection export the expected hosts without adding filtered-out hosts; list/card views and keyboard activation agree.
2. Name/IP order is deterministic for nested groups, duplicate names, mixed case, IPv4/IPv6 and domains; switching back to Manual restores the preexisting order.
3. A selects an order and changes manual order, then A and B synchronize through fake WebDAV: B obtains the preference and manual data, while each device retains its fold state. Restart and search/filter preserve the selected order.
4. Native headless driver exercises the controls and exports at wide/narrow sizes, both views, light/dark themes and current locales. Screenshots are visually inspected.
5. Focused regression, repository verification and a fresh independent code review precede PR submission. Environment-dependent failures are reported with their retry results.
