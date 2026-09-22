import { useEffect, useState } from 'react';
import { EventsOn } from '../lib/wailsRuntime';

// BrowserView embeds a live view of the Agent's browser page for one session by
// rendering the screencast frames that arrive over the desktop/browser/frame
// event. It is the same page the Agent drives (streamed over CDP), not a second
// instance. The view is read-only in this slice; the page also stays open in its
// own OS window. Frames are base64 JPEG.
export default function BrowserView({ sessionId }) {
  const [frame, setFrame] = useState(null);

  useEffect(() => {
    setFrame(null);
    const off = EventsOn('desktop/browser/frame', (p) => {
      if (!p || p.session_id !== sessionId) return;
      setFrame(p.data || null);
    });
    return () => off?.();
  }, [sessionId]);

  if (!frame) {
    return (
      <div className="browser-view browser-view--empty">
        Waiting for the page… the browser also opens in its own window.
      </div>
    );
  }
  return (
    <div className="browser-view">
      <img className="browser-view__frame" src={`data:image/jpeg;base64,${frame}`} alt="Live browser page" />
    </div>
  );
}
