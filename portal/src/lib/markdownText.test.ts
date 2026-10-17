import { describe, expect, it } from "vitest"
import { markdownToPlainText } from "./markdownText"

describe("markdownToPlainText", () => {
  it("keeps the words of headings and task lists and drops their syntax", () => {
    expect(
      markdownToPlainText(
        "## Acceptance criteria\n\n- [ ] Review documentation\n- [x] Verify installation\n\nParent issue with mixed child progress.",
      ),
    ).toBe("Acceptance criteria Review documentation Verify installation Parent issue with mixed child progress.")
  })

  it("drops emphasis, link, and code markers but keeps their text", () => {
    expect(markdownToPlainText("Read **the** [brief](https://example.com/brief) and run `make`.")).toBe(
      "Read the brief and run make.",
    )
    expect(markdownToPlainText("检查中文、Markdown 和长文本展示。\n\n```sh\n./make kind fixtures\n```")).toBe(
      "检查中文、Markdown 和长文本展示。 ./make kind fixtures",
    )
  })

  it("drops raw HTML, keeps an image's alternative text, and reads a table by cell", () => {
    expect(markdownToPlainText("<details>hidden</details>\n\n![a diagram](d.png)")).toBe("a diagram")
    expect(markdownToPlainText("| Step | Owner |\n| --- | --- |\n| Draft | Bob |")).toBe("Step Owner Draft Bob")
  })

  it("leaves plain text alone and bounds a long one", () => {
    expect(markdownToPlainText("Check a fresh workspace with no existing settings.")).toBe(
      "Check a fresh workspace with no existing settings.",
    )
    const long = markdownToPlainText("word ".repeat(100), 20)
    expect(long).toHaveLength(20)
    expect(long.endsWith("…")).toBe(true)
    expect(markdownToPlainText("")).toBe("")
  })
})
