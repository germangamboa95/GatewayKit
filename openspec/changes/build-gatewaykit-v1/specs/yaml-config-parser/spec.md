# Spec Delta

## Purpose

Parses `gateway.yaml` with a built-in parser for the YAML subset the config schema uses, since the build allows no outside modules. Rejects anything outside that subset with a line number instead of misreading it.

## ADDED Requirements

### Requirement: Parse block mappings
The parser SHALL parse block mappings written as `key: value`, indented with spaces. Any indent width MUST be accepted as long as it is consistent within a block. A tab character in the indentation MUST cause a parse error that names the line.

#### Scenario: Nested mapping with two-space indent
- **WHEN** the file contains `gateway:` followed by `  port: 8080` on the next line
- **THEN** the parser returns a map whose `gateway` key holds a map with `port` set to the string `8080`

#### Scenario: Four-space indent is accepted
- **WHEN** a nested mapping is indented with four spaces throughout
- **THEN** the parser returns the same tree as the two-space version

#### Scenario: Tab in indentation
- **WHEN** a line's indentation contains a tab character
- **THEN** parsing fails with an error that names that line

### Requirement: Parse block sequences
The parser SHALL parse block sequences written as `- item`. Items MUST be allowed to be scalars or mappings, where `- key: value` starts a mapping item. The `-` MUST be accepted at its parent key's indentation or deeper.

#### Scenario: Sequence of mappings at parent indentation
- **WHEN** the file contains `routes:` followed by `- path: "/x"` at the same indentation and `  upstream:` beneath it
- **THEN** the parser returns `routes` as a list with one map holding `path` and `upstream`

#### Scenario: Sequence indented deeper than its parent key
- **WHEN** the `-` of each item sits two spaces deeper than `routes:`
- **THEN** the parser returns the same list as when the `-` sits at the parent's indentation

#### Scenario: Sequence of scalars
- **WHEN** a key holds the items `- a` and `- b`
- **THEN** the parser returns a list of the strings `a` and `b`

### Requirement: Parse single-line flow collections
The parser SHALL parse flow sequences `[a, "b", 3]` and flow mappings `{ k: v }`, including nested flow collections. A flow collection MUST open and close on the same line; one that spans lines MUST cause a parse error that names the line.

#### Scenario: Flow sequence
- **WHEN** a value is `["GET", "POST"]`
- **THEN** the parser returns a list of the strings `GET` and `POST`, each marked as quoted

#### Scenario: Nested flow mapping
- **WHEN** a value is `{ add: {}, remove: [] }`
- **THEN** the parser returns a map whose `add` is an empty map and whose `remove` is an empty list

#### Scenario: Flow collection spanning lines
- **WHEN** a flow sequence opens with `[` on one line and closes with `]` on a later line
- **THEN** parsing fails with an error that names the line where it opened

### Requirement: Parse scalar styles
The parser SHALL parse plain, double-quoted and single-quoted scalars. Double-quoted scalars MUST support the escapes `\"`, `\\`, `\n`, `\t` and `\uXXXX`; any other escape MUST be an error. In single-quoted scalars `''` MUST produce one `'`.

#### Scenario: Double-quoted escapes
- **WHEN** a value is `"a\"b\\c\nd\teé"`
- **THEN** the parser returns the string with a quote, a backslash, a newline, a tab and `é` in place of the escapes

#### Scenario: Single-quoted doubled quote
- **WHEN** a value is `'it''s'`
- **THEN** the parser returns the string `it's`

#### Scenario: Plain scalar
- **WHEN** a value is `fixed_window`
- **THEN** the parser returns the string `fixed_window`, marked as not quoted

### Requirement: Strip comments
The parser SHALL treat `#` as the start of a comment when it is at the start of a line or follows whitespace, and is outside quotes. A `#` inside a quoted scalar or directly after a non-space character MUST stay part of the value.

#### Scenario: Trailing comment
- **WHEN** a line is `timeout: "5s" # Override global timeout`
- **THEN** the parser returns `timeout` as `5s` and ignores the comment

#### Scenario: Hash inside quotes
- **WHEN** a value is `"a # b"`
- **THEN** the parser returns the string `a # b`

#### Scenario: Full-line comment
- **WHEN** a line starts with `#` after optional indentation
- **THEN** the parser ignores the whole line

### Requirement: Treat empty values as null
The parser SHALL return a null node for a key with nothing after the colon and nothing nested under it.

#### Scenario: Empty value
- **WHEN** the file contains `global_rate_limit:` followed by a line at the same or lower indentation
- **THEN** the parser returns `global_rate_limit` as a null node

### Requirement: Accept any scalar as a key
The parser SHALL accept any plain or quoted scalar as a mapping key, including keys that contain dots or dashes.

#### Scenario: Dotted and dashed keys
- **WHEN** a mapping contains the keys `user.id` and `X-Gateway`
- **THEN** the parser returns a map with the literal keys `user.id` and `X-Gateway`

#### Scenario: Quoted key
- **WHEN** a mapping contains the key `"X-Request-Start"`
- **THEN** the parser returns a map with the key `X-Request-Start`

### Requirement: Reject duplicate keys
The parser MUST fail when a key appears more than once in the same mapping, naming the line of the repeated key.

#### Scenario: Repeated key
- **WHEN** one mapping contains `port: 8080` and later `port: 9090`
- **THEN** parsing fails with an error that names the line of the second `port`

#### Scenario: Same key in different mappings
- **WHEN** two different routes each contain a `path` key
- **THEN** parsing succeeds

### Requirement: Ignore document markers
The parser SHALL ignore a leading `---` line and a trailing `...` line.

#### Scenario: Leading and trailing markers
- **WHEN** the file starts with `---` and ends with `...`
- **THEN** the parser returns the same tree as the file without those lines

### Requirement: Reject unsupported YAML features
The parser MUST fail, naming the line, on anchors and aliases (`&`, `*`, `<<`), tags such as `!!str`, block scalars (`|`, `>`), more than one document in a file, complex keys (`?`) and plain values that span several lines. It MUST never silently misread such input.

#### Scenario: Anchor
- **WHEN** a line contains `users: &mock`
- **THEN** parsing fails with an error that names that line

#### Scenario: Merge key
- **WHEN** a line contains `<<: *mock`
- **THEN** parsing fails with an error that names that line

#### Scenario: Tag
- **WHEN** a value is `!!str 8080`
- **THEN** parsing fails with an error that names that line

#### Scenario: Block scalar
- **WHEN** a value is `|` followed by indented text lines
- **THEN** parsing fails with an error that names the line with `|`

#### Scenario: Second document
- **WHEN** a `---` line appears after content
- **THEN** parsing fails with an error that names that line

#### Scenario: Complex key
- **WHEN** a line starts with `? `
- **THEN** parsing fails with an error that names that line

#### Scenario: Multi-line plain value
- **WHEN** a plain value continues on the next, more-indented line
- **THEN** parsing fails with an error that names the continuation line

### Requirement: Produce an untyped positioned tree
The parser SHALL return only strings, maps, lists and nulls. Each string MUST record whether it was quoted. Every node MUST carry its line and column. Map keys MUST keep their file order. The parser MUST NOT convert scalars to numbers or booleans.

#### Scenario: Numbers stay strings
- **WHEN** a value is `8080`
- **THEN** the parser returns the string `8080`, marked as not quoted

#### Scenario: Positions and key order
- **WHEN** a mapping lists `port`, then `global_timeout`, then `global_rate_limit`
- **THEN** the returned map lists its keys in that order and each node reports its line and column

#### Scenario: Parse error location
- **WHEN** parsing fails
- **THEN** the error message carries the line and column of the problem
