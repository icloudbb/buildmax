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
import { chatMessages } from "./chat"
import { tasksMessages } from "./tasks"
import { workflowsMessages } from "./workflows"
import { agentsMessages } from "./agents"
import { schedulesMessages } from "./schedules"

// One file per area, each holding both languages, so a string and its
// translation change together. Terms follow the glossary in
// docs/design/ui-experience-program.md (D5).
export const portalMessages = mergeMessages(
  shellMessages,
  statusMessages,
  helpMessages,
  chatMessages,
  issuesMessages,
  runsMessages,
  filesMessages,
  artifactsMessages,
  marketplaceMessages,
  commonMessages,
  authMessages,
  tasksMessages,
  workflowsMessages,
  agentsMessages,
  schedulesMessages,
)

export type MessageKey = keyof typeof portalMessages.en

export const { useT, useStableT, translate } = createTranslator(portalMessages)
