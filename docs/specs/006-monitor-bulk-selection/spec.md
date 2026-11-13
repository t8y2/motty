# Monitor Bulk Selection And Ordering

Version: 1.0

## Authority

- GitHub issue: https://github.com/t8y2/motty/issues/782.
- The user authorized implementation, Viva headless validation, evidence retention,
  commit, push and a pull request on 2026-11-13.
- Visual basis: existing monitor editor, host picker and host catalog controls.

## Scope

1. The monitor editor offers one tri-state checkbox for all eligible SSH hosts
   matching its current search, group and selected-only filters, including matches
   below the viewport. Clearing it affects only that scope. Existing selections
   outside the scope and unavailable saved hosts are preserved. Empty scopes are
   disabled. Local, RDP, VNC and Mosh hosts are not newly selected.
2. The existing Arrange hosts tab offers manual, name and IP order. Name and IP
   actions reuse the host catalog's language-aware name and numeric IP comparators.
   Ties are deterministic, and unavailable saved hosts remain after known hosts.
3. Sorting changes only the draft selected host list. Cancel changes nothing;
   Save persists the resulting order through the existing panel Hosts array, used
   by both Simple and Detailed views. No schema or collection lifecycle changes.
4. Sorting is an explicit arrangement action, not an ongoing live sorting policy.
   Reopening starts in manual/current-order mode; moving a host manually switches
   the editor back to manual. Changing search or group never sorts implicitly.
5. Pointer and keyboard operation, menu dismissal, narrow windows, light/dark
   appearance and English/Chinese text remain usable. Opening or saving a stopped
   panel never starts remote collection.

## Acceptance

- A 96-host fixture selects all filtered matches, not only painted cards, without
  duplicates; partial selection is indeterminate and off-filter selections survive.
- Name, IPv4/IPv6/hostname order, deterministic ties and unavailable entries are
  exercised with literal expected host IDs.
- Headless pointer/key input changes draft order, cancellation preserves the panel,
  saving/reloading preserves order, and existing manual reorder remains functional.
- Capture actual Viva headless frames at desktop and narrow sizes and inspect them.
- Save red/green, focused/race, full-suite, vet/build results with exact base and
  pinned Viva versions. Publish a small reproducible screenshot set with the PR.

## Non-Goals

No new monitoring protocols, connection attempts, collection settings, global host
order changes, live automatic reordering or framework workarounds.
