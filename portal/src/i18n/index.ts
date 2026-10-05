import { createTranslator, mergeMessages } from "@buildmax/gui"
import { shellMessages } from "./shell"
import { statusMessages } from "./status"
import { helpMessages } from "./help"
import { issuesMessages } from "./issues"
import { runsMessages } from "./runs"

// One file per area, each holding both languages, so a string and its
// translation change together. Terms follow the glossary in
// docs/design/ui-experience-program.md (D5).
export const portalMessages = mergeMessages(
  shellMessages,
  statusMessages,
  helpMessages,
  issuesMessages,
  runsMessages,
)

export type MessageKey = keyof typeof portalMessages.en

export const { useT, translate } = createTranslator(portalMessages)
