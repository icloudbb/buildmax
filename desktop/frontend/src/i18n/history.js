// The rewind and fork picker, and what the user is told once a move is made.
// Tool names are identifiers and stay as the Go side reports them.
export const historyMessages = {
  en: {
    'history.title': 'History',
    'history.tabs': 'What to do with the chosen message',
    'history.rewind': 'Rewind',
    'history.rewindHint': 'Take a prompt back to send again',
    'history.fork': 'Fork',
    'history.forkHint': 'Start a new session from here',
    'history.nothingToFork': 'Nothing to fork from yet.',
    'history.noPrompt': 'No prompt to take back yet.',
    'history.messages': 'Messages',
    'history.noText': '(no text)',
    'history.forkHere': 'Fork here',
    'history.takeBack': 'Take this prompt back',

    'history.who.agent': 'agent',
    'history.who.event': 'event',
    'history.who.you': 'you',
    'history.interrupted': '{name} (interrupted)',

    'history.forkCopies': 'copies this conversation up to here into a new session',
    'history.forkUnaware': 'new session starts here · will not know about: {tools}',
    'history.rewindRemoves': {
      one: 'prompt comes back · removes {count} message',
      other: 'prompt comes back · removes {count} messages',
    },
    'history.rewindNothingRan': '{removes} · nothing outside the conversation ran',
    'history.rewindLeaves': '{removes} · leaves in place: {tools}',

    'history.report.forkClean': 'The original session is unchanged, and nothing outside the conversation ran after this point.',
    'history.report.forkTools': 'The original session is unchanged. These ran after the fork point, so their effects are on disk but the new session\'s history does not mention them: {tools}',
    'history.report.rewindTools': 'These ran before the rewind and their effects are still in place: {tools}. Rewinding moves the conversation. It does not undo files, commands, or network calls.',
    'history.report.rewindClean': 'Nothing outside the conversation ran in the part that was rewound, so there is nothing left over.',
    // The prompt notes follow the sentence above, so English opens with a space.
    'history.report.draftKept': ' The composer already held a draft, so the rewound prompt was left out of it.',
    'history.report.promptBack': ' The prompt is back in the composer.',
    'history.report.imagesLost': {
      one: ' The prompt is back in the composer. Its {count} image did not come back.',
      other: ' The prompt is back in the composer. Its {count} images did not come back.',
    },
  },
  'zh-CN': {
    'history.title': '历史',
    'history.tabs': '如何处理所选消息',
    'history.rewind': '回退',
    'history.rewindHint': '撤回一条提示词以重新发送',
    'history.fork': '分叉',
    'history.forkHint': '从这里开始一个新会话',
    'history.nothingToFork': '还没有可以分叉的位置。',
    'history.noPrompt': '还没有可以撤回的提示词。',
    'history.messages': '消息',
    'history.noText': '（无文本）',
    'history.forkHere': '从这里分叉',
    'history.takeBack': '撤回这条提示词',

    'history.who.agent': 'Agent',
    'history.who.event': '事件',
    'history.who.you': '你',
    'history.interrupted': '{name}（已中断）',

    'history.forkCopies': '将截至此处的对话复制到新会话',
    'history.forkUnaware': '新会话从这里开始 · 不会知道：{tools}',
    'history.rewindRemoves': '提示词撤回 · 移除 {count} 条消息',
    'history.rewindNothingRan': '{removes} · 对话之外没有执行任何操作',
    'history.rewindLeaves': '{removes} · 保留以下操作的效果：{tools}',

    'history.report.forkClean': '原会话保持不变，且此处之后没有在对话之外执行任何操作。',
    'history.report.forkTools': '原会话保持不变。以下操作在分叉点之后执行，其效果已落到磁盘，但新会话的历史中不会提及：{tools}',
    'history.report.rewindTools': '以下操作在回退前已执行，其效果仍然保留：{tools}。回退只移动对话，不会撤销文件、命令或网络调用。',
    'history.report.rewindClean': '被回退的部分没有在对话之外执行任何操作，因此没有遗留影响。',
    'history.report.draftKept': '输入框中已有草稿，因此撤回的提示词没有放入其中。',
    'history.report.promptBack': '提示词已放回输入框。',
    'history.report.imagesLost': '提示词已放回输入框，但其中的 {count} 张图片没有恢复。',
  },
};
