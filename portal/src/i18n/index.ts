import { createTranslator, mergeMessages } from "@buildmax/gui"
import { shellMessages } from "./shell"
import { statusMessages } from "./status"
import { helpMessages } from "./help"
import { issuesMessages } from "./issues"
import { runsMessages } from "./runs"
import { filesMessages } from "./files"
import { artifactsMessages } from "./artifacts"
import { marketplaceMessages } from "./marketplace"
import { commonMessages } from "./common"
import { authMessages } from "./auth"

// One file per area, each holding both languages, so a string and its
// translation change together. Terms follow the glossary in
// docs/design/ui-experience-program.md (D5).
export const portalMessages = mergeMessages(
  shellMessages,
  statusMessages,
  helpMessages,
  issuesMessages,
  runsMessages,
  filesMessages,
  artifactsMessages,
  marketplaceMessages,
  commonMessages,
  authMessages,
)

export type MessageKey = keyof typeof portalMessages.en

export const { useT, useStableT, translate } = createTranslator(portalMessages)
