// Port of frontend/src/pages/TerminalPage.tsx: xterm.js session over the
// backend's WebSocket PTY proxy. Auth rides on the HttpOnly session cookie.
(function () {
  function start() {
    var root = document.getElementById('terminal-root');
    if (!root) return;
    var workspaceName = root.dataset.workspace;
    var agentId = root.dataset.agentId;
    var statusEl = document.getElementById('terminal-status');
    var errorEl = document.getElementById('terminal-error');
    var host = document.getElementById('terminal-container');

    var setStatus = function (text) { statusEl.textContent = text; };
    var setError = function (text) {
      if (!text) {
        errorEl.classList.add('hidden');
        return;
      }
      errorEl.textContent = text;
      errorEl.classList.remove('hidden');
    };

    var term = new Terminal({
      cursorBlink: true,
      fontFamily: '"IBM Plex Mono", ui-monospace, monospace',
      fontSize: 13,
      scrollback: 5000,
      theme: { background: '#0f1c1a', foreground: '#e7efec', cursor: '#5eead4' },
    });
    var fitAddon = new FitAddon.FitAddon();
    term.loadAddon(fitAddon);
    term.open(host);
    fitAddon.fit();

    var protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    var params = new URLSearchParams({
      height: String(term.rows),
      width: String(term.cols),
    });
    if (agentId) params.set('agent_id', agentId);
    var socket = new WebSocket(
      protocol + '//' + window.location.host +
      '/api/workspaces/' + encodeURIComponent(workspaceName) + '/terminal?' + params
    );
    socket.binaryType = 'arraybuffer';

    var encoder = new TextEncoder();
    var decoder = new TextDecoder();

    var sendResize = function () {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(encoder.encode(JSON.stringify({ height: term.rows, width: term.cols })));
      }
    };

    socket.onopen = function () {
      setStatus('connected');
      setError(null);
      sendResize();
    };
    socket.onmessage = function (event) {
      if (typeof event.data === 'string') {
        term.write(event.data);
        return;
      }
      term.write(decoder.decode(event.data));
    };
    socket.onerror = function () {
      setStatus('error');
      setError('WebSocket connection failed');
    };
    socket.onclose = function (event) {
      if (event.code !== 1000) {
        setStatus('error');
        setError(event.reason || 'Disconnected (' + event.code + ')');
      }
    };

    term.onData(function (data) {
      if (socket.readyState === WebSocket.OPEN) {
        socket.send(encoder.encode(JSON.stringify({ data: data })));
      }
    });

    window.addEventListener('resize', function () {
      fitAddon.fit();
      sendResize();
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', start);
  } else {
    start();
  }
})();
