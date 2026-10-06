# Interface migration

The visual reference is the installed 3x-ui v3.9.0. Use its Ant Design 6
dark theme and the same Ant Design icons instead of custom approximations.

Palette: layout #1a1b1f, sidebar #15161a, container #23252b,
elevated #2d2f37, primary Ant Design blue, text from the dark algorithm.
Typography: Ant Design's system font and compact table typography.
Layout: fixed 220px sidebar / 72px rail, 24px content padding, configuration
state card above task content. Routing uses the three reference tabs.

Navigation and data remain specific to a home gateway. Preserve existing
SQLite data, backup format, rule ordering and explicit Xray application.
All seven pages use React components; no embedded legacy HTML pages.
Prepared static assets are embedded in the Go executable, without a Node
runtime on installed VMs. Keep CSS limited to layout rather than replacing
Ant Design controls. Verify mutations with a copied database.
