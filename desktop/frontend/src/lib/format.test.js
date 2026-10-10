import { describe, expect, it } from 'vitest'
import { buildDiffTree, parsePatchLines, toolCallTarget } from './format.js'

describe('toolCallTarget', () => {
  it('names the file a call acts on, in full', () => {
    expect(toolCallTarget('{"file_path":"src/main.go"}')).toEqual({ text: 'src/main.go', isPath: true })
    const long = 'internal/scheduler/lease/renewal/renewal_with_a_long_name_test.go'
    expect(toolCallTarget(JSON.stringify({ file_path: long, offset: 10 }))).toEqual({ text: long, isPath: true })
  })

  it('never falls back to the raw JSON', () => {
    expect(toolCallTarget('{"content":"","file_path":"NOTES.txt"}')).toEqual({ text: 'NOTES.txt', isPath: true })
    expect(toolCallTarget('{"replace_all":true}').text).toBe('')
    expect(toolCallTarget('{"content":"').text).toBe('')
  })

  it('shows the first line of a command', () => {
    expect(toolCallTarget({ command: 'go test ./...\ngo vet ./...' })).toEqual({ text: 'go test ./... …', isPath: false })
  })
})

const TWO_HUNK_PATCH = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1,2 +1,2 @@
-old first
+new first
 context one
@@ -10,2 +10,2 @@
-old second
+new second
 context two`

describe('parsePatchLines', () => {
  it('tags header rows with no hunk yet', () => {
    const rows = parsePatchLines(TWO_HUNK_PATCH)
    const headerRows = rows.filter((r) => r.kind === 'header')
    expect(headerRows.every((r) => r.hunk === -1)).toBe(true)
  })

  it('groups rows under the hunk they belong to', () => {
    const rows = parsePatchLines(TWO_HUNK_PATCH)
    const firstHunkRows = rows.filter((r) => r.kind !== 'header' && r.hunk === 0)
    const secondHunkRows = rows.filter((r) => r.kind !== 'header' && r.hunk === 1)
    expect(firstHunkRows.map((r) => r.text)).toEqual(['@@ -1,2 +1,2 @@', '-old first', '+new first', ' context one'])
    expect(secondHunkRows.map((r) => r.text)).toEqual(['@@ -10,2 +10,2 @@', '-old second', '+new second', ' context two'])
  })
})

describe('buildDiffTree', () => {
  const file = (path) => ({ path, status: 'modified', additions: 1, deletions: 0 })

  it('nests files under their directories, dirs before files, sorted by name', () => {
    const tree = buildDiffTree([
      file('README.md'),
      file('src/main.go'),
      file('src/app.go'),
      file('cmd/root.go'),
    ])
    // Two dirs (cmd, src) sorted first, then the root file.
    expect(tree.map((n) => [n.type, n.name])).toEqual([
      ['dir', 'cmd'],
      ['dir', 'src'],
      ['file', 'README.md'],
    ])
    const src = tree.find((n) => n.name === 'src')
    expect(src.children.map((n) => [n.type, n.name])).toEqual([
      ['file', 'app.go'],
      ['file', 'main.go'],
    ])
  })

  it('merges single-child directory chains into one compact row', () => {
    const tree = buildDiffTree([file('a/b/c/deep.go')])
    expect(tree).toHaveLength(1)
    expect(tree[0].type).toBe('dir')
    expect(tree[0].name).toBe('a/b/c')
    expect(tree[0].path).toBe('a/b/c')
    expect(tree[0].children.map((n) => n.name)).toEqual(['deep.go'])
  })

  it('stops compacting where a directory branches', () => {
    const tree = buildDiffTree([file('a/b/one.go'), file('a/c/two.go')])
    expect(tree).toHaveLength(1)
    expect(tree[0].name).toBe('a')
    expect(tree[0].children.map((n) => [n.type, n.name])).toEqual([
      ['dir', 'b'],
      ['dir', 'c'],
    ])
  })
})
