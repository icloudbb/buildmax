import { createTranslator, mergeMessages } from '@buildmax/gui';
import { chatMessages } from './chat';
import { homeMessages } from './home';
import { shellMessages } from './shell';

// One file per area, each holding both languages, so a string and its
// translation change together. Terms follow the glossary in
// docs/design/ui-experience-program.md (D5).
export const desktopMessages = mergeMessages(shellMessages, homeMessages, chatMessages);

export const { useT, useStableT, translate } = createTranslator(desktopMessages);
