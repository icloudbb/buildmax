import { unified } from "unified"
import remarkParse from "remark-parse"
import remarkGfm from "remark-gfm"

// The parser react-markdown renders with, so a snippet and the rendered
// description agree on what is syntax and what is text.
const parser = unified().use(remarkParse).use(remarkGfm)

/** The part of an mdast node a plain-text reading needs. */
interface MarkdownNode {
  type: string
  value?: string
  alt?: string | null
  children?: MarkdownNode[]
}

/** Nodes whose text stands apart from the next block's, as a space. */
const blockTypes = new Set([
  "blockquote",
  "code",
  "heading",
  "list",
  "listItem",
  "paragraph",
  "table",
  "tableCell",
  "tableRow",
])

/** A snippet never needs more source than this; parsing stays bounded. */
const SOURCE_LIMIT = 4000

/**
 * The words of a Markdown text, without its syntax: headings, list markers,
 * task boxes, emphasis, links, and code fences are dropped and the text they
 * held is kept, on one line. Raw HTML is markup, so it is dropped too; an
 * image keeps its alternative text. Longer than `maxChars`, it ends in "…".
 */
export function markdownToPlainText(markdown: string, maxChars = 200): string {
  const tree = parser.parse(markdown.slice(0, SOURCE_LIMIT)) as MarkdownNode
  const parts: string[] = []
  const walk = (node: MarkdownNode) => {
    switch (node.type) {
      case "text":
      case "inlineCode":
      case "code":
        parts.push(node.value ?? "")
        break
      case "image":
      case "imageReference":
        if (node.alt) parts.push(node.alt)
        break
      case "html":
        break
      case "break":
        parts.push(" ")
        break
      default:
        node.children?.forEach(walk)
    }
    if (blockTypes.has(node.type)) parts.push(" ")
  }
  walk(tree)
  const text = parts.join("").replace(/\s+/g, " ").trim()
  return text.length > maxChars ? `${text.slice(0, maxChars - 1).trimEnd()}…` : text
}
