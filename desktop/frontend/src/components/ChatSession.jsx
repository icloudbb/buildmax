import { useState, useRef, useEffect, useCallback } from 'react';
import { ChatThread } from '@buildmax/gui';
import { shortToolArgs } from '../lib/format';
import { addLiveToolCall, addLiveToolResult, appendAssistantForNextLLM, mergeRunStatus } from '../lib/messages';
import { messageThreadItems } from '../lib/threadItems';
import { EventsOn } from '../lib/wailsRuntime';
import { MarkdownMessage } from './MarkdownMessage';
import { InfoPanel } from './InfoPanel';
import { ChatInput } from './ChatInput';
import { useStableT, useT } from '../i18n';

// A session without a title names its tab with this. The tab title is saved in
// the workspace layout, so it stays English there and TabBar translates it.
const UNTITLED = 'Chat';

const EV_STREAM_DELTA = 'desktop/stream-delta';
const EV_STREAM_DONE = 'desktop/stream-done';
const EV_STREAM_ERROR = 'desktop/stream-error';
const EV_SESSION_ADOPTED = 'desktop/session-adopted';
const EV_LLM_START = 'desktop/llm-start';
const EV_TOOL_START = 'desktop/tool-start';
const EV_TOOL_END = 'desktop/tool-end';
const EV_RUN_STATUS = 'desktop/run-status';
const EV_MESSAGE_DEQUEUED = 'desktop/message-dequeued';
const EV_MESSAGE_BLOCKED = 'desktop/message-blocked';
const EV_JOB_DELIVERY = 'desktop/job-delivery';
const EV_JOB_DELIVERY_PENDING = 'desktop/job-delivery-pending';
const EV_TURN_DIGEST = 'desktop/turn-digest';

// ChatSession owns one session's chat: its transcript, run state, and the stream
// it listens to. Each chat tab renders its own instance, so several sessions run
// and display at once. Every stream event carries a session_id (see the desktop
// backend); an instance handles only its own. A brand-new chat starts with no id
// and adopts the real one from the first tagged event of the run it launched
// (only one new chat exists per project, so the adoption is unambiguous).
//
// approvals maps session id to that session's pending tool approval; this tab
// shows only its own and answers it through onRespond(request, decision). Only
// the focused pane's tab takes approval keystrokes, so a key press never answers
// two sessions' prompts at once. questions and onAnswer(request, answer) do the
// same for the Agent's AskUser questions.
export function ChatSession({
  projectId, projectName, defaultWorkspace, sessions, tab, app,
  approvals, onRespond, questions, onAnswer, focused = true,
  onSessionAdopted, onSessionsChanged, onTitle, onOpenSession, onShowChanges,
  draft = null, onDraftConsumed,
}) {
  const t = useT();
  const stableT = useStableT();
  const [messages, setMessages] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);
  const [runStatus, setRunStatus] = useState(null);
  const [toolActivity, setToolActivity] = useState('');
  const [queuedMessages, setQueuedMessages] = useState([]);
  const [turnDigest, setTurnDigest] = useState(null);
  const [historyNotice, setHistoryNotice] = useState(null);
  const [infoOpen, setInfoOpen] = useState(false);
  const [sessionId, setSessionId] = useState(tab.sessionId || '');

  const historyRef = useRef(null);
  const streamingContentRef = useRef('');
  // The live session id, read inside event handlers without re-subscribing.
  const sessionIdRef = useRef(sessionId);
  // True from launching a new chat until its run's id is adopted.
  const adoptingRef = useRef(false);
  // The id this tab adopted from its own run. Its transcript is already on
  // screen, and the session on disk does not hold the prompt yet when the id is
  // announced, so loading it would replace the sent message with nothing.
  const adoptedIdRef = useRef('');
  useEffect(() => { sessionIdRef.current = sessionId; }, [sessionId]);

  // ownEvent is true when a stream event belongs to this session. Adoption of a
  // new chat's id happens only via the dedicated session-adopted event (below),
  // never here, so a concurrently running session's events are never mistaken for
  // this new chat's.
  const ownEvent = useCallback((payload) => {
    const sid = payload?.session_id ?? '';
    return sid !== '' && sid === sessionIdRef.current;
  }, []);

  // Load (or clear) the transcript when the bound session changes.
  useEffect(() => {
    if (!sessionId) {
      setMessages([]);
      return;
    }
    if (!app || sessionId === adoptedIdRef.current) return;
    setError(null);
    app.GetSession(sessionId)
      .then((detail) => {
        setMessages(detail?.messages ?? []);
        const title = detail?.title?.trim() || UNTITLED;
        onTitle?.(tab, title);
      })
      .catch((err) => setError(err?.message ?? String(err)));
    // onTitle is stable enough; re-running only on session change is intended.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sessionId, app]);

  // Read the run status and queue for this session when it settles. A scheduled
  // run opened from the Schedules view has no project but does have a session, so
  // this runs whenever there is an app — the backend resolves the host by session.
  useEffect(() => {
    if (!app || (!projectId && !sessionId)) { setRunStatus(null); return; }
    app.GetRunStatus(projectId, sessionId || '')
      .then((status) => setRunStatus(status ?? null))
      .catch(() => setRunStatus(null));
  }, [app, projectId, sessionId]);

  useEffect(() => {
    if (!app || typeof app.QueuedMessages !== 'function') { setQueuedMessages([]); return undefined; }
    let stale = false;
    app.QueuedMessages(projectId, sessionId || '')
      .then((list) => { if (!stale) setQueuedMessages(list ?? []); })
      .catch(() => {});
    return () => { stale = true; };
  }, [app, projectId, sessionId]);

  // Subscribe once; every handler filters to this session via ownEvent.
  useEffect(() => {
    // A new chat adopts the id its run created. Only new-chat runs emit this, and
    // one exists at a time, so the pending new-chat tab adopts unambiguously.
    const unsubAdopted = EventsOn(EV_SESSION_ADOPTED, (payload) => {
      const sid = payload?.session_id ?? '';
      if (sid && sessionIdRef.current === '' && adoptingRef.current) {
        sessionIdRef.current = sid;
        adoptedIdRef.current = sid;
        adoptingRef.current = false;
        setSessionId(sid);
        onSessionAdopted?.(tab, sid);
      }
    });
    const unsubDelta = EventsOn(EV_STREAM_DELTA, (payload) => {
      if (!ownEvent(payload)) return;
      const delta = payload?.delta;
      if (typeof delta !== 'string') return;
      streamingContentRef.current += delta;
      setMessages((prev) => {
        const next = [...prev];
        const last = next[next.length - 1];
        if (last?.role === 'assistant') {
          next[next.length - 1] = { ...last, content: streamingContentRef.current };
        }
        return next;
      });
    });
    const unsubDone = EventsOn(EV_STREAM_DONE, (payload) => {
      if (!ownEvent(payload)) return;
      const reply = payload?.reply ?? streamingContentRef.current;
      const doneId = payload?.session_id ?? sessionIdRef.current;
      streamingContentRef.current = '';
      setToolActivity('');
      setRunStatus({
        context_tokens: payload?.context_tokens ?? 0,
        context_window: payload?.context_window ?? 0,
        prompt_tokens: payload?.prompt_tokens ?? 0,
        completion_tokens: payload?.completion_tokens ?? 0,
        total_prompt_tokens: payload?.total_prompt_tokens ?? 0,
        total_completion_tokens: payload?.total_completion_tokens ?? 0,
      });
      setMessages((prev) => {
        const next = [...prev];
        const last = next[next.length - 1];
        if (last?.role === 'assistant') {
          if (!reply) return next.slice(0, -1);
          next[next.length - 1] = { ...last, content: reply };
        }
        return next;
      });
      if (doneId) {
        app?.GetSession(doneId).then((detail) => {
          setMessages(detail?.messages ?? []);
          const title = detail?.title?.trim() || UNTITLED;
          onTitle?.(tab, title);
        }).catch(() => {});
      }
      onSessionsChanged?.();
      setQueuedMessages([]);
      setLoading(false);
    });
    const unsubError = EventsOn(EV_STREAM_ERROR, (payload) => {
      if (!ownEvent(payload)) return;
      streamingContentRef.current = '';
      setToolActivity('');
      setError(payload?.message ?? stableT('chat.streamError'));
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (last?.role === 'assistant' && last?.content === '') return prev.slice(0, -1);
        return prev;
      });
      setLoading(false);
    });
    const unsubLLMStart = EventsOn(EV_LLM_START, (payload) => {
      if (!ownEvent(payload)) return;
      streamingContentRef.current = '';
      setMessages((prev) => appendAssistantForNextLLM(prev));
    });
    const unsubToolStart = EventsOn(EV_TOOL_START, (payload) => {
      if (!ownEvent(payload)) return;
      const name = payload?.tool_name ?? '';
      const args = payload?.args ? shortToolArgs(payload.args) : '';
      setToolActivity(args ? `⚙ ${name} (${args})` : `⚙ ${name}`);
      if (payload?.tool_call_id) {
        setMessages((prev) => addLiveToolCall(prev, {
          id: payload.tool_call_id,
          name,
          arguments: payload?.args || '',
        }));
      }
    });
    const unsubToolEnd = EventsOn(EV_TOOL_END, (payload) => {
      if (!ownEvent(payload)) return;
      setToolActivity('');
      setMessages((prev) => addLiveToolResult(prev, payload));
    });
    const unsubRunStatus = EventsOn(EV_RUN_STATUS, (payload) => {
      if (!ownEvent(payload)) return;
      setRunStatus((prev) => mergeRunStatus(prev, payload));
    });
    const unsubDequeued = EventsOn(EV_MESSAGE_DEQUEUED, (payload) => {
      if (!ownEvent(payload)) return;
      setQueuedMessages(payload?.queued ?? []);
      const prompt = payload?.prompt ?? '';
      if (!prompt) return;
      setError(null);
      setLoading(true);
      streamingContentRef.current = '';
      setMessages((prev) => [...prev, { role: 'user', content: prompt }]);
    });
    const unsubBlocked = EventsOn(EV_MESSAGE_BLOCKED, (payload) => {
      if (!ownEvent(payload)) return;
      setQueuedMessages(payload?.queued ?? []);
      setError(stableT('chat.blocked', { reason: payload?.reason ?? stableT('chat.noReason') }));
    });
    const unsubJobDelivery = EventsOn(EV_JOB_DELIVERY, (payload) => {
      if ((payload?.session_id ?? '') !== sessionIdRef.current || !sessionIdRef.current) return;
      setError(null);
      setLoading(true);
      streamingContentRef.current = '';
      setMessages((prev) => [...prev, {
        role: 'user',
        source: payload?.source || 'background_event',
        content: stableT('chat.jobDelivery', {
          source: payload?.source ?? stableT('chat.backgroundEvent'),
          job: payload?.job_id ?? '',
          title: payload?.title ?? '',
        }),
      }]);
    });
    const unsubTurnDigest = EventsOn(EV_TURN_DIGEST, (payload) => {
      if (!ownEvent(payload)) return;
      setTurnDigest({ recap: payload?.recap ?? '', suggestion: payload?.suggestion ?? '' });
    });
    return () => {
      unsubAdopted?.(); unsubDelta?.(); unsubDone?.(); unsubError?.(); unsubLLMStart?.();
      unsubToolStart?.(); unsubToolEnd?.(); unsubRunStatus?.(); unsubDequeued?.();
      unsubBlocked?.(); unsubJobDelivery?.(); unsubTurnDigest?.();
    };
  }, [ownEvent, app, tab, onSessionAdopted, onSessionsChanged, onTitle, stableT]);

  // Pull parked background deliveries whenever this session is idle and on screen.
  // A projectless scheduled session has no jobs, so skip it entirely.
  useEffect(() => {
    if (loading || !sessionId || !projectId || !app?.DeliverNextJobEvent) return undefined;
    const pull = () => { app.DeliverNextJobEvent(projectId, sessionId).catch(() => {}); };
    pull();
    const unsub = EventsOn(EV_JOB_DELIVERY_PENDING, (p) => {
      if (p?.project_id === projectId && (p?.session_id ?? '') === sessionId) pull();
    });
    return unsub;
  }, [loading, sessionId, projectId, app]);

  useEffect(() => {
    if (historyRef.current) {
      historyRef.current.scrollTop = historyRef.current.scrollHeight;
    }
  }, [messages]);

  const reloadSession = useCallback(() => {
    if (!app || !sessionId) return;
    app.GetSession(sessionId)
      .then((detail) => {
        setMessages(detail?.messages ?? []);
      })
      .catch((err) => setError(err?.message ?? String(err)));
  }, [app, sessionId]);

  function handleRewound(report) {
    reloadSession();
    setHistoryNotice({ kind: 'rewind', text: report });
    setTurnDigest(null);
  }

  function handleCompacted(result) {
    reloadSession();
    const summarized = result?.summarized ?? 0;
    const text = summarized > 0
      ? t('chat.compactResult', {
          count: summarized,
          kept: result?.kept ?? 0,
          after: result?.after_tokens ?? 0,
          before: result?.before_tokens ?? 0,
        })
      : (result?.reason || t('chat.nothingToCompact'));
    setHistoryNotice({ kind: 'compact', text });
    setTurnDigest(null);
    if (app && sessionId) {
      app.GetRunStatus(projectId, sessionId)
        .then((status) => setRunStatus((prev) => mergeRunStatus(prev, status)))
        .catch(() => {});
    }
  }

  // A fork is a different session: open it in its own tab rather than replacing
  // this transcript.
  function handleForked(newSessionId) {
    onSessionsChanged?.();
    onOpenSession?.(newSessionId);
  }

  async function handleSend(prompt) {
    if (!prompt?.trim() || !app) return;
    const trimmed = prompt.trim();
    const sid = sessionIdRef.current;

    if (loading) {
      try {
        const position = await app.SendMessageStream(projectId, sid, trimmed);
        if (position > 0) setQueuedMessages((prev) => [...prev, trimmed]);
      } catch (err) {
        setError(err?.message ?? String(err));
      }
      return;
    }

    setLoading(true);
    setError(null);
    setHistoryNotice(null);
    setTurnDigest(null);
    streamingContentRef.current = '';
    if (sid === '') adoptingRef.current = true;
    setRunStatus((prev) => ({ ...(prev ?? {}), prompt_tokens: 0, completion_tokens: 0 }));
    setMessages((prev) => [
      ...prev,
      { role: 'user', content: trimmed },
      { role: 'assistant', content: '' },
    ]);
    try {
      await app.SendMessageStream(projectId, sid, trimmed);
    } catch (err) {
      adoptingRef.current = false;
      setError(err?.message ?? String(err));
      setMessages((prev) => {
        const last = prev[prev.length - 1];
        if (last?.role === 'assistant' && last?.content === '') return prev.slice(0, -1);
        return prev;
      });
      setLoading(false);
    }
  }

  async function handleCancel() {
    if (!loading || !app) return;
    setQueuedMessages([]);
    try {
      await app.CancelRun(projectId, sessionIdRef.current);
    } catch (err) {
      console.error('CancelRun failed:', err);
    }
  }

  const threadItems = messageThreadItems(messages, t);

  if (historyNotice) {
    threadItems.push({
      id: 'history-notice',
      role: 'system',
      label: historyNotice.kind === 'fork' ? t('chat.forked')
        : historyNotice.kind === 'compact' ? t('chat.compacted')
        : t('chat.rewound'),
      hideAvatar: true,
      body: <div className="page-chat__msg-content page-chat__history-notice">{historyNotice.text}</div>,
    });
  }
  for (const [i, queued] of queuedMessages.entries()) {
    threadItems.push({
      id: `queued-${i}`,
      role: 'user',
      label: t('chat.queued'),
      hideAvatar: true,
      body: (
        <div className="page-chat__msg-content page-chat__msg-content--queued">
          <MarkdownMessage content={queued} />
        </div>
      ),
    });
  }
  if (turnDigest?.recap) {
    threadItems.push({
      id: 'turn-recap',
      role: 'notice',
      label: t('chat.turnRecap'),
      hideAvatar: true,
      body: <div className="page-chat__recap">{turnDigest.recap}</div>,
    });
  }

  const workspace = sessions?.find((s) => s.id === sessionId)?.workspace || defaultWorkspace;

  const approvalRequest = (sessionId && approvals?.[sessionId]) || null;
  const respondToApproval = (decision) => onRespond?.(approvalRequest, decision);
  const questionRequest = (sessionId && questions?.[sessionId]) || null;
  const answerQuestion = (answer) => onAnswer?.(questionRequest, answer);

  return (
    <div className="page-chat">
      {infoOpen && (
        <div className="chat-info" aria-label={t('chat.sessionInfo')}>
          <div className="chat-info__head">
            <span className="chat-info__title">{t('chat.sessionInfo')}</span>
            <button
              type="button"
              className="chat-info__close"
              onClick={() => setInfoOpen(false)}
              title={t('shell.close')}
              aria-label={t('chat.closeSessionInfo')}
            >
              ✕
            </button>
          </div>
          <div className="chat-info__body">
            <InfoPanel
              projectID={projectId}
              sessionID={sessionId || ''}
              projectName={projectName}
              workspace={workspace}
              app={app}
            />
          </div>
        </div>
      )}
      <ChatThread
        historyRef={historyRef}
        ariaLabel={t('chat.history')}
        items={threadItems}
        emptyText={t('chat.empty')}
      />
      <section className="page-chat__input" aria-label={t('chat.sendMessage')}>
        <ChatInput
          onSend={handleSend}
          onCancel={handleCancel}
          draft={draft}
          onDraftConsumed={onDraftConsumed}
          loading={loading}
          error={error}
          onDismissError={() => setError(null)}
          currentProject={{ id: projectId, name: projectName }}
          app={app}
          approvalRequest={approvalRequest}
          onRespond={respondToApproval}
          approvalKeys={focused}
          questionRequest={questionRequest}
          onAnswer={answerQuestion}
          toolActivity={toolActivity}
          runStatus={runStatus}
          suggestion={turnDigest?.suggestion ?? ''}
          onAcceptSuggestion={() => setTurnDigest(null)}
          sessionId={sessionId || ''}
          onShowInfo={() => setInfoOpen(true)}
          onShowChanges={onShowChanges}
          infoOpen={infoOpen}
          onToggleInfo={() => setInfoOpen((v) => !v)}
          onRewound={handleRewound}
          onForked={handleForked}
          onCompacted={handleCompacted}
          onCommandError={(msg) => setError(msg)}
          onRunStatusContext={(status) => {
            setRunStatus((prev) => ({
              ...(status ?? {}),
              prompt_tokens: prev?.prompt_tokens ?? 0,
              completion_tokens: prev?.completion_tokens ?? 0,
              total_prompt_tokens: prev?.total_prompt_tokens ?? status?.total_prompt_tokens ?? 0,
              total_completion_tokens: prev?.total_completion_tokens ?? status?.total_completion_tokens ?? 0,
            }));
          }}
        />
      </section>
    </div>
  );
}
