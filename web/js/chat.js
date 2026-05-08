(function () {
    const csrfToken = sessionStorage.getItem('csrf_token');
    const currentUserID = parseInt(sessionStorage.getItem('user_id'));
    if (!csrfToken || !currentUserID) { window.location.href = '/'; return; }

    // Sakura petals (fewer on mobile)
    (function spawnPetals() {
        const isMobile = window.innerWidth < 600;
        const COUNT = isMobile ? 6 : 15;
        for (let i = 0; i < COUNT; i++) {
            const p = document.createElement('div');
            p.className = 'petal';
            p.style.left = Math.random() * 100 + 'vw';
            p.style.animationDuration = (6 + Math.random() * 8) + 's';
            p.style.animationDelay = (Math.random() * 10) + 's';
            p.style.width = (6 + Math.random() * 8) + 'px';
            p.style.height = p.style.width;
            document.body.appendChild(p);
        }
    })();

    // DOM refs
    const $ = (id) => document.getElementById(id);
    const messageList = $('message-list');
    const messageInput = $('message-input');
    const emptyState = $('empty-state');
    const loadMoreDiv = $('load-more');
    const typingIndicator = $('typing-indicator');
    const reconnectBanner = $('reconnect-banner');
    const uploadProgress = $('upload-progress');
    const uploadBar = $('upload-bar');
    const toast = $('toast');
    const lightbox = $('lightbox');
    const lightboxImg = $('lightbox-img');
    const contextMenu = $('context-menu');
    const dropOverlay = $('drop-overlay');
    const searchBar = $('search-bar');
    const searchResults = $('search-results');
    const replyPreview = $('reply-preview');
    const voiceRecording = $('voice-recording');
    const voiceTimer = $('voice-timer');

    let ws = null, reconnectDelay = 1000, wsConnected = false;
    let oldestMessageID = null, hasMore = false;
    let typingTimeout = null, lastTypingSent = 0;
    let isPageVisible = true, unreadCount = 0;
    let lastDateLabel = null;
    let replyingTo = null;
    let contextMsgID = null, contextMsgUserID = null;
    let mediaRecorder = null, voiceChunks = [], voiceStart = 0, voiceInterval = null;
    let peerLastRead = 0;
    let peerTypingClear = null;
    let offlineQueue = [];
    let idleTimer = null;
    const IDLE_TIMEOUT = 60 * 60 * 1000; // 1 hour
    const msgCache = {};

    // Notification sound (generated beep)
    const notifSound = (() => {
        try {
            const ctx = new (window.AudioContext || window.webkitAudioContext)();
            return () => {
                const osc = ctx.createOscillator();
                const gain = ctx.createGain();
                osc.connect(gain); gain.connect(ctx.destination);
                osc.frequency.value = 800;
                gain.gain.value = 0.1;
                osc.start(); osc.stop(ctx.currentTime + 0.1);
            };
        } catch { return () => {}; }
    })();

    // Theme
    const savedTheme = localStorage.getItem('whisper_theme') || 'dark';
    if (savedTheme === 'light') document.body.classList.add('light');
    $('theme-toggle').addEventListener('click', () => {
        document.body.classList.toggle('light');
        const isLight = document.body.classList.contains('light');
        localStorage.setItem('whisper_theme', isLight ? 'light' : 'dark');
        $('theme-label').textContent = isLight ? 'Dark mode' : 'Light mode';
    });
    if (savedTheme === 'light') { const tl = $('theme-label'); if (tl) tl.textContent = 'Dark mode'; }

    // Overflow menu
    $('menu-toggle').addEventListener('click', (e) => {
        e.stopPropagation();
        const menu = $('overflow-menu');
        menu.hidden = !menu.hidden;
    });
    document.addEventListener('click', () => { const m = $('overflow-menu'); if (m) m.hidden = true; });

    // Sound toggle
    let soundEnabled = localStorage.getItem('whisper_sound') !== 'off';
    function updateSoundLabel() {
        const lbl = $('sound-label');
        if (lbl) lbl.textContent = soundEnabled ? 'Mute' : 'Unmute';
    }
    updateSoundLabel();
    $('sound-toggle').addEventListener('click', () => {
        soundEnabled = !soundEnabled;
        localStorage.setItem('whisper_sound', soundEnabled ? 'on' : 'off');
        updateSoundLabel();
        showToast(soundEnabled ? 'Sound on' : 'Sound muted');
    });

    // PWA
    if ('serviceWorker' in navigator) {
        navigator.serviceWorker.register('/sw.js').catch(() => {});
    }

    // Version check
    let serverVersion = null;
    async function checkVersion() {
        try {
            const r = await fetch('/api/version');
            const d = await r.json();
            if (serverVersion && serverVersion !== d.version) {
                showToast('New version available — click to refresh');
                toast.style.cursor = 'pointer';
                toast.addEventListener('click', () => location.reload(), { once: true });
            }
            serverVersion = d.version;
        } catch {}
    }
    checkVersion();
    setInterval(checkVersion, 5 * 60 * 1000); // check every 5 min

    // Page visibility
    document.addEventListener('visibilitychange', () => {
        isPageVisible = !document.hidden;
        if (isPageVisible) {
            unreadCount = 0; document.title = 'Whisper';
            hasUnreadSep = false;
            const sep = $('unread-sep');
            if (sep) setTimeout(() => sep.remove(), 3000);
        }
    });

    // Init
    loadMessages().then(() => connectWS());
    messageInput.focus();

    // Event listeners
    $('send-btn').addEventListener('click', sendMessage);
    messageInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMessage(); }
    });
    messageInput.addEventListener('input', () => {
        sendTyping();
        // Auto-grow textarea
        messageInput.style.height = 'auto';
        messageInput.style.height = Math.min(messageInput.scrollHeight, 120) + 'px';
    });
    $('attach-btn').addEventListener('click', () => $('file-input').click());
    $('file-input').addEventListener('change', handleFileUpload);
    $('load-more-btn').addEventListener('click', () => loadMessages(oldestMessageID));
    $('logout-btn').addEventListener('click', logout);

    // Clipboard paste (images)
    messageInput.addEventListener('paste', (e) => {
        const items = e.clipboardData?.items;
        if (!items) return;
        for (const item of items) {
            if (item.type.startsWith('image/')) {
                e.preventDefault();
                const file = item.getAsFile();
                if (file) uploadFile(file);
                return;
            }
        }
    });

    // Drag & drop
    const chatArea = $('messages');
    let dragCounter = 0;
    chatArea.addEventListener('dragenter', (e) => { e.preventDefault(); dragCounter++; dropOverlay.hidden = false; });
    chatArea.addEventListener('dragleave', () => { dragCounter--; if (dragCounter <= 0) { dropOverlay.hidden = true; dragCounter = 0; } });
    chatArea.addEventListener('dragover', (e) => e.preventDefault());
    chatArea.addEventListener('drop', (e) => {
        e.preventDefault(); dropOverlay.hidden = true; dragCounter = 0;
        if (e.dataTransfer.files.length) uploadFile(e.dataTransfer.files[0]);
    });

    // Lightbox
    $('lightbox-close').addEventListener('click', () => lightbox.hidden = true);
    lightbox.addEventListener('click', (e) => { if (e.target === lightbox) lightbox.hidden = true; });
    document.addEventListener('keydown', (e) => { if (e.key === 'Escape') { lightbox.hidden = true; contextMenu.hidden = true; searchBar.hidden = true; searchResults.hidden = true; } });

    // Search
    $('search-toggle').addEventListener('click', () => {
        const open = searchBar.hidden;
        searchBar.hidden = !open;
        if (open) $('search-input').focus();
        else { searchResults.hidden = true; $('search-input').value = ''; }
    });
    $('search-close').addEventListener('click', () => { searchBar.hidden = true; searchResults.hidden = true; });
    let searchDebounce = null;
    $('search-input').addEventListener('input', (e) => {
        clearTimeout(searchDebounce);
        searchDebounce = setTimeout(() => doSearch(e.target.value), 300);
    });

    // Context menu (reactions, reply, delete, copy)
    function openContextMenu(x, y, msgEl) {
        contextMsgID = parseInt(msgEl.dataset.id);
        contextMsgUserID = parseInt(msgEl.dataset.uid);
        const del = contextMenu.querySelector('[data-action="delete"]');
        del.hidden = contextMsgUserID !== currentUserID;
        contextMenu.style.left = Math.min(x, window.innerWidth - 180) + 'px';
        contextMenu.style.top = Math.min(y, window.innerHeight - 250) + 'px';
        contextMenu.hidden = false;
    }
    // Desktop: right-click
    document.addEventListener('contextmenu', (e) => {
        const msgEl = e.target.closest('.message');
        if (!msgEl) return;
        e.preventDefault();
        openContextMenu(e.clientX, e.clientY, msgEl);
    });
    // Mobile: long-press (touch-hold)
    let touchTimer = null, touchMsgEl = null;
    document.addEventListener('touchstart', (e) => {
        const msgEl = e.target.closest('.message');
        if (!msgEl) return;
        touchMsgEl = msgEl;
        touchTimer = setTimeout(() => {
            const t = e.touches[0];
            openContextMenu(t.clientX, t.clientY, msgEl);
            touchMsgEl = null;
        }, 500);
    }, { passive: true });
    document.addEventListener('touchmove', () => { clearTimeout(touchTimer); }, { passive: true });
    document.addEventListener('touchend', () => { clearTimeout(touchTimer); }, { passive: true });

    document.addEventListener('click', () => contextMenu.hidden = true);
    contextMenu.addEventListener('click', (e) => {
        const btn = e.target.closest('button');
        if (!btn || !contextMsgID) return;
        const action = btn.dataset.action;
        if (action === 'reply') startReply(contextMsgID);
        else if (action === 'react') wsSend({ type: 'reaction', message_id: contextMsgID, emoji: btn.dataset.emoji });
        else if (action === 'delete') wsSend({ type: 'delete', message_id: contextMsgID });
        else if (action === 'copy') {
            const msg = msgCache[contextMsgID];
            if (msg?.content) { navigator.clipboard.writeText(msg.content); showToast('Copied'); }
        }
        contextMenu.hidden = true;
    });

    // Gallery
    $('gallery-btn').addEventListener('click', openGallery);
    $('gallery-close').addEventListener('click', () => $('gallery-modal').hidden = true);
    async function openGallery() {
        try {
            const r = await fetch('/api/media');
            const d = await r.json();
            const grid = $('gallery-grid');
            grid.innerHTML = '';
            if (!d.media?.length) { grid.innerHTML = '<p style="color:var(--text-muted);text-align:center;padding:2rem">No media shared yet</p>'; }
            else {
                d.media.forEach((m) => {
                    if (m.content_type.startsWith('image/')) {
                        const img = document.createElement('img');
                        img.src = '/api/media/' + m.id;
                        img.className = 'gallery-item';
                        img.loading = 'lazy';
                        img.addEventListener('click', () => openLightbox(img.src));
                        grid.appendChild(img);
                    } else {
                        const div = document.createElement('a');
                        div.href = '/api/media/' + m.id;
                        div.target = '_blank';
                        div.className = 'gallery-file-item';
                        div.textContent = m.filename;
                        grid.appendChild(div);
                    }
                });
            }
            $('gallery-modal').hidden = false;
        } catch { showToast('Failed to load gallery', 'error'); }
    }

    // Ping measurement
    let lastPing = 0;
    function measurePing() {
        if (!ws || ws.readyState !== WebSocket.OPEN) return;
        lastPing = Date.now();
        wsSend({ type: 'ping' });
    }
    setInterval(measurePing, 15000);

    // Reply
    $('reply-cancel').addEventListener('click', cancelReply);

    // Voice recording
    $('voice-btn').addEventListener('click', toggleVoice);
    $('voice-cancel').addEventListener('click', stopVoice);
    $('voice-send').addEventListener('click', sendVoice);

    // Export
    $('export-btn').addEventListener('click', () => { window.location.href = '/api/messages/export'; });

    // Wallpaper
    $('wallpaper-btn').addEventListener('click', () => $('wallpaper-input').click());
    $('wallpaper-input').addEventListener('change', (e) => {
        const file = e.target.files[0];
        if (!file) return;
        const reader = new FileReader();
        reader.onload = () => {
            localStorage.setItem('whisper_wallpaper', reader.result);
            applyWallpaper();
        };
        reader.readAsDataURL(file);
        e.target.value = '';
    });
    // Double-click wallpaper button to remove
    $('wallpaper-btn').addEventListener('dblclick', () => {
        localStorage.removeItem('whisper_wallpaper');
        $('messages').style.backgroundImage = '';
        showToast('Wallpaper removed');
    });
    function applyWallpaper() {
        const wp = localStorage.getItem('whisper_wallpaper');
        if (wp) {
            $('messages').style.backgroundImage = `url(${wp})`;
            $('messages').style.backgroundSize = 'cover';
            $('messages').style.backgroundPosition = 'center';
        }
    }
    applyWallpaper();

    // Scroll-to-bottom button
    const scrollBtn = $('scroll-bottom-btn');
    const scrollBadge = $('scroll-badge');
    let newMsgCount = 0;
    chatArea.addEventListener('scroll', () => {
        const atBot = isAtBottom();
        scrollBtn.hidden = atBot;
        if (atBot) { newMsgCount = 0; scrollBadge.hidden = true; }
    });
    scrollBtn.addEventListener('click', () => { scrollBottom(); scrollBtn.hidden = true; newMsgCount = 0; scrollBadge.hidden = true; });

    // Auto-logout on idle
    function resetIdle() {
        clearTimeout(idleTimer);
        idleTimer = setTimeout(() => { logout(); }, IDLE_TIMEOUT);
    }
    ['click', 'keydown', 'mousemove', 'touchstart'].forEach(e => document.addEventListener(e, resetIdle, { passive: true }));
    resetIdle();

    // Request notification permission
    if ('Notification' in window && Notification.permission === 'default') {
        Notification.requestPermission();
    }

    // === FUNCTIONS ===

    async function loadMessages(before) {
        let url = '/api/messages?limit=50';
        if (before) url += '&before=' + before;
        try {
            const res = await fetch(url);
            if (res.status === 401) { sessionStorage.clear(); window.location.href = '/'; return; }
            const data = await res.json();
            hasMore = data.has_more;
            loadMoreDiv.hidden = !hasMore;
            if (data.messages?.length > 0) {
                hideEmpty();
                const frag = document.createDocumentFragment();
                let prevDate = null;
                data.messages.forEach((msg) => {
                    msgCache[msg.id] = msg;
                    const d = formatDateLabel(msg.created_at);
                    if (d !== prevDate) { frag.appendChild(dateSep(d)); prevDate = d; }
                    frag.appendChild(createMsgEl(msg));
                });
                if (before) {
                    const sh = chatArea.scrollHeight;
                    messageList.prepend(frag);
                    chatArea.scrollTop = chatArea.scrollHeight - sh;
                } else {
                    messageList.appendChild(frag);
                    lastDateLabel = prevDate;
                    scrollBottom();
                }
                oldestMessageID = data.messages[0].id;
            }
        } catch (err) { console.error('Load failed:', err); }
    }

    function connectWS() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(`${proto}//${location.host}/ws`);
        ws.onopen = () => { wsConnected = true; reconnectBanner.hidden = true; reconnectDelay = 1000; flushOfflineQueue(); };
        ws.onmessage = (e) => { try { handleWS(JSON.parse(e.data)); } catch {} };
        ws.onclose = (ev) => {
            const was = wsConnected; wsConnected = false;
            if (ev.code === 1008 || ev.code === 4001) { sessionStorage.clear(); window.location.href = '/'; return; }
            if (was) reconnectBanner.hidden = false;
            setTimeout(() => { reconnectBanner.hidden = false; reconnectDelay = Math.min(reconnectDelay * 2, 30000); connectWS(); }, reconnectDelay);
        };
        ws.onerror = () => {};
    }

    function wsSend(obj) {
        if (ws?.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify(obj));
        } else if (obj.type === 'message') {
            offlineQueue.push(obj);
            showToast('Queued — will send when reconnected');
        }
    }
    function flushOfflineQueue() {
        while (offlineQueue.length > 0) {
            const msg = offlineQueue.shift();
            if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
        }
    }

    function handleWS(msg) {
        switch (msg.type) {
            case 'message': onNewMessage(msg); break;
            case 'typing': onTyping(msg); break;
            case 'presence': onPresence(msg); break;
            case 'reaction': onReaction(msg); break;
            case 'delete': onDelete(msg); break;
            case 'read': onRead(msg); break;
            case 'pong':
                if (lastPing) { const ms = Date.now() - lastPing; const pd = $('ping-display'); if (pd) pd.textContent = ms + 'ms'; lastPing = 0; }
                break;
            case 'error': showToast(msg.content, 'error'); break;
        }
    }

    let hasUnreadSep = false;
    function onNewMessage(msg) {
        hideEmpty();
        msgCache[msg.id] = msg;
        const atBot = isAtBottom();
        const d = formatDateLabel(msg.created_at);
        if (d !== lastDateLabel) { messageList.appendChild(dateSep(d)); lastDateLabel = d; }
        // Unread separator
        if (!isPageVisible && msg.user.id !== currentUserID && !hasUnreadSep) {
            const sep = document.createElement('div');
            sep.className = 'unread-separator';
            sep.id = 'unread-sep';
            sep.innerHTML = '<span>New messages</span>';
            messageList.appendChild(sep);
            hasUnreadSep = true;
        }
        messageList.appendChild(createMsgEl(msg));
        if (atBot) { scrollBottom(); }
        else if (msg.user.id !== currentUserID) {
            // Show badge on scroll button
            newMsgCount++;
            scrollBadge.textContent = newMsgCount;
            scrollBadge.hidden = false;
            scrollBtn.hidden = false;
        }
        if (!isPageVisible && msg.user.id !== currentUserID) {
            unreadCount++;
            document.title = `(${unreadCount}) Whisper`;
            if (soundEnabled) notifSound();
            // Browser push notification
            if ('Notification' in window && Notification.permission === 'granted') {
                new Notification('Whisper', { body: msg.content || 'New message', tag: 'whisper-msg', silent: true });
            }
        }
        // Send read receipt
        if (isPageVisible && msg.user.id !== currentUserID) {
            wsSend({ type: 'read', last_read_id: msg.id });
        }
    }

    function onTyping(msg) {
        if (msg.user.id === currentUserID) return;
        const t = msg.data?.is_typing;
        typingIndicator.hidden = !t;
        if (t) {
            $('typing-name').textContent = msg.user.username;
            // Auto-clear typing after 5s in case they close browser
            clearTimeout(peerTypingClear);
            peerTypingClear = setTimeout(() => { typingIndicator.hidden = true; }, 5000);
        } else {
            clearTimeout(peerTypingClear);
        }
    }

    function onPresence(msg) {
        if (msg.user.id === currentUserID) return;
        $('peer-name').textContent = msg.user.username;
        $('peer-dot').className = 'dot ' + (msg.data?.online ? 'online' : 'offline');
    }

    function onReaction(msg) {
        const el = messageList.querySelector(`[data-id="${msg.id}"]`);
        if (!el) return;
        let rc = el.querySelector('.reactions');
        if (rc) rc.remove();
        const reactions = msg.data?.reactions;
        if (reactions?.length) {
            rc = document.createElement('div');
            rc.className = 'reactions';
            reactions.forEach((r) => {
                const btn = document.createElement('button');
                btn.className = 'reaction-btn' + (r.user_id === currentUserID ? ' mine' : '');
                btn.textContent = r.emoji;
                btn.addEventListener('click', () => wsSend({ type: 'reaction', message_id: msg.id, emoji: r.emoji }));
                rc.appendChild(btn);
            });
            el.querySelector('.bubble').appendChild(rc);
        }
    }

    function onDelete(msg) {
        const el = messageList.querySelector(`[data-id="${msg.id}"]`);
        if (!el) return;
        const bubble = el.querySelector('.bubble');
        bubble.innerHTML = '';
        const p = document.createElement('p');
        p.className = 'text deleted';
        p.textContent = 'Message deleted';
        bubble.appendChild(p);
        el.classList.add('deleted');
    }

    function onRead(msg) {
        if (msg.user.id === currentUserID) return;
        peerLastRead = msg.data?.last_read_id || 0;
        // Update read indicators
        messageList.querySelectorAll('.message.mine .read-indicator').forEach((el) => {
            const mid = parseInt(el.closest('.message').dataset.id);
            el.textContent = mid <= peerLastRead ? '✓✓' : '✓';
            el.className = 'read-indicator' + (mid <= peerLastRead ? ' read' : '');
        });
    }

    function createMsgEl(msg) {
        const isMine = msg.user.id === currentUserID;
        const div = document.createElement('div');
        div.className = 'message ' + (isMine ? 'mine' : 'theirs') + (msg.deleted ? ' deleted' : '');
        div.dataset.id = msg.id;
        div.dataset.uid = msg.user.id;

        const bubble = document.createElement('div');
        bubble.className = 'bubble';

        // Sender name (for group chats / non-mine messages)
        if (!isMine) {
            const prev = messageList.lastElementChild;
            const prevUid = prev?.dataset?.uid;
            if (!prevUid || parseInt(prevUid) !== msg.user.id) {
                const nameLabel = document.createElement('span');
                nameLabel.className = 'sender-name';
                nameLabel.textContent = msg.user.username;
                bubble.appendChild(nameLabel);
            }
        }

        // Reply preview
        if (msg.reply_to) {
            const rp = document.createElement('div');
            rp.className = 'reply-quote';
            const rn = document.createElement('span');
            rn.className = 'reply-quote-name';
            rn.textContent = msg.reply_to.user?.username || '';
            rp.appendChild(rn);
            const rt = document.createElement('span');
            rt.className = 'reply-quote-text';
            rt.textContent = msg.reply_to.content?.slice(0, 80) || '';
            rp.appendChild(rt);
            rp.addEventListener('click', () => scrollToMsg(msg.reply_to.id));
            bubble.appendChild(rp);
        }

        if (msg.deleted) {
            const p = document.createElement('p');
            p.className = 'text deleted';
            p.textContent = 'Message deleted';
            bubble.appendChild(p);
        } else {
            // Media
            if (msg.media) {
                if (msg.media.content_type.startsWith('image/')) {
                    const wrap = document.createElement('div');
                    wrap.className = 'media-image-wrap';
                    const img = document.createElement('img');
                    img.src = '/api/media/' + msg.media.id;
                    img.alt = msg.media.filename;
                    img.className = 'media-image';
                    img.loading = 'lazy';
                    img.addEventListener('click', () => openLightbox(img.src));
                    wrap.appendChild(img);
                    bubble.appendChild(wrap);
                } else if (msg.media.content_type.startsWith('audio/') || msg.kind === 'voice') {
                    const audio = document.createElement('audio');
                    audio.controls = true;
                    audio.src = '/api/media/' + msg.media.id;
                    audio.className = 'media-audio';
                    bubble.appendChild(audio);
                } else {
                    const link = document.createElement('a');
                    link.href = '/api/media/' + msg.media.id;
                    link.className = 'media-file';
                    link.target = '_blank';
                    const s1 = document.createElement('span');
                    s1.textContent = msg.media.filename;
                    const s2 = document.createElement('span');
                    s2.className = 'media-size';
                    s2.textContent = fmtSize(msg.media.size_bytes);
                    link.appendChild(s1);
                    link.appendChild(s2);
                    bubble.appendChild(link);
                }
            }

            // Text with markdown + links
            if (msg.content) {
                const p = document.createElement('p');
                p.className = 'text';
                p.innerHTML = renderText(msg.content);
                bubble.appendChild(p);
            }

            // Reactions
            if (msg.reactions?.length) {
                const rc = document.createElement('div');
                rc.className = 'reactions';
                msg.reactions.forEach((r) => {
                    const btn = document.createElement('button');
                    btn.className = 'reaction-btn' + (r.user_id === currentUserID ? ' mine' : '');
                    btn.textContent = r.emoji;
                    btn.addEventListener('click', () => wsSend({ type: 'reaction', message_id: msg.id, emoji: r.emoji }));
                    rc.appendChild(btn);
                });
                bubble.appendChild(rc);
            }
        }

        // Time + read indicator (added to every bubble, CSS hides on grouped)
        const timeRow = document.createElement('span');
        timeRow.className = 'time';
        timeRow.textContent = fmtTime(msg.created_at);
        if (isMine && !msg.deleted) {
            const ri = document.createElement('span');
            ri.className = 'read-indicator' + (peerLastRead >= msg.id ? ' read' : '');
            ri.textContent = peerLastRead >= msg.id ? '✓✓' : '✓';
            timeRow.appendChild(ri);
        }
        bubble.appendChild(timeRow);
        // Mark for grouping (prev message from same user)
        const prev = messageList.lastElementChild;
        if (prev?.classList.contains('message') && parseInt(prev.dataset.uid) === msg.user.id) {
            div.classList.add('grouped');
        }

        div.appendChild(bubble);

        if (!isMine) $('peer-name').textContent = msg.user.username;
        return div;
    }

    // Markdown + links
    function renderText(text) {
        // Escape HTML first
        let s = text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        // Code blocks ```
        s = s.replace(/```([\s\S]*?)```/g, '<code class="block">$1</code>');
        // Inline code `
        s = s.replace(/`([^`]+)`/g, '<code>$1</code>');
        // Bold **text**
        s = s.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
        // Italic *text*
        s = s.replace(/\*(.+?)\*/g, '<em>$1</em>');
        // Strikethrough ~~text~~
        s = s.replace(/~~(.+?)~~/g, '<del>$1</del>');
        // Links
        s = s.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>');
        // Emoji shortcodes
        s = s.replace(/:([a-z0-9_+-]+):/g, (m, code) => emojiMap[code] || m);
        return s;
    }

    const emojiMap = {smile:'😊',laugh:'😂',heart:'❤️',thumbsup:'👍',thumbsdown:'👎',fire:'🔥',check:'✅',x:'❌',eyes:'👀',think:'🤔',clap:'👏',wave:'👋',ok:'👌',pray:'🙏',rocket:'🚀',star:'⭐',warning:'⚠️',skull:'💀',cry:'😢',angry:'😠',cool:'😎',wink:'😉',love:'😍',poop:'💩',100:'💯',tada:'🎉'};

    function sendMessage() {
        const content = messageInput.value.trim();
        if (!content || !ws || ws.readyState !== WebSocket.OPEN) return;
        const msg = { type: 'message', content, media_id: null };
        if (replyingTo) { msg.reply_to_id = replyingTo; cancelReply(); }
        wsSend(msg);
        messageInput.value = '';
        messageInput.style.height = 'auto';
        messageInput.focus();
        sendTypingStop();
    }

    function sendTyping() {
        if (!ws || ws.readyState !== WebSocket.OPEN) return;
        const now = Date.now();
        if (now - lastTypingSent < 2000) return;
        lastTypingSent = now;
        wsSend({ type: 'typing', is_typing: true });
        clearTimeout(typingTimeout);
        typingTimeout = setTimeout(sendTypingStop, 3000);
    }

    function sendTypingStop() {
        wsSend({ type: 'typing', is_typing: false });
        clearTimeout(typingTimeout);
    }

    // Reply
    function startReply(msgID) {
        replyingTo = msgID;
        const msg = msgCache[msgID];
        $('reply-preview-name').textContent = msg?.user?.username || '';
        $('reply-preview-text').textContent = (msg?.content || '').slice(0, 80) || '[media]';
        replyPreview.hidden = false;
        messageInput.focus();
    }
    function cancelReply() { replyingTo = null; replyPreview.hidden = true; }

    // File upload
    function handleFileUpload() {
        const f = $('file-input').files[0];
        if (f) uploadFile(f);
        $('file-input').value = '';
    }

    async function uploadFile(file) {
        uploadProgress.hidden = false;
        uploadBar.style.width = '0%';
        try {
            const fd = new FormData();
            fd.append('file', file);
            const xhr = new XMLHttpRequest();
            xhr.open('POST', '/api/media/upload');
            xhr.setRequestHeader('X-CSRF-Token', csrfToken);
            xhr.upload.onprogress = (e) => { if (e.lengthComputable) uploadBar.style.width = Math.round(e.loaded / e.total * 100) + '%'; };
            const res = await new Promise((ok, fail) => {
                xhr.onload = () => xhr.status === 200 ? ok(JSON.parse(xhr.responseText)) : fail(new Error(JSON.parse(xhr.responseText).error || 'Upload failed'));
                xhr.onerror = () => fail(new Error('Upload failed'));
                xhr.send(fd);
            });
            uploadProgress.hidden = true;
            const msg = { type: 'message', content: '', media_id: res.media_id };
            if (replyingTo) { msg.reply_to_id = replyingTo; cancelReply(); }
            wsSend(msg);
        } catch (err) { uploadProgress.hidden = true; showToast(err.message, 'error'); }
    }

    // Voice recording
    async function toggleVoice() {
        if (mediaRecorder) { stopVoice(); return; }
        try {
            const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
            mediaRecorder = new MediaRecorder(stream, { mimeType: getSupportedMime() });
            voiceChunks = [];
            mediaRecorder.ondataavailable = (e) => { if (e.data.size) voiceChunks.push(e.data); };
            mediaRecorder.start();
            voiceStart = Date.now();
            voiceRecording.hidden = false;
            voiceInterval = setInterval(() => {
                const s = Math.floor((Date.now() - voiceStart) / 1000);
                voiceTimer.textContent = Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
            }, 200);
        } catch { showToast('Microphone access denied', 'error'); }
    }

    function stopVoice() {
        if (mediaRecorder) { mediaRecorder.stop(); mediaRecorder.stream.getTracks().forEach(t => t.stop()); mediaRecorder = null; }
        clearInterval(voiceInterval);
        voiceRecording.hidden = true;
        voiceChunks = [];
    }

    async function sendVoice() {
        if (!mediaRecorder) return;
        mediaRecorder.stop();
        mediaRecorder.stream.getTracks().forEach(t => t.stop());
        await new Promise(r => setTimeout(r, 100)); // let ondataavailable fire
        const blob = new Blob(voiceChunks, { type: voiceChunks[0]?.type || 'audio/webm' });
        mediaRecorder = null;
        clearInterval(voiceInterval);
        voiceRecording.hidden = true;
        const file = new File([blob], 'voice.webm', { type: blob.type });
        uploadFile(file);
    }

    function getSupportedMime() {
        for (const m of ['audio/webm;codecs=opus', 'audio/webm', 'audio/ogg', 'audio/mp4']) {
            if (MediaRecorder.isTypeSupported(m)) return m;
        }
        return '';
    }

    // Search
    async function doSearch(q) {
        if (!q.trim()) { searchResults.hidden = true; return; }
        try {
            const res = await fetch('/api/messages/search?q=' + encodeURIComponent(q));
            const data = await res.json();
            searchResults.innerHTML = '';
            if (data.messages?.length) {
                data.messages.forEach((m) => {
                    const div = document.createElement('div');
                    div.className = 'search-result';
                    div.innerHTML = `<strong>${esc(m.user.username)}</strong> <span class="search-result-text">${esc(m.content.slice(0, 100))}</span>`;
                    div.addEventListener('click', () => { scrollToMsg(m.id); searchBar.hidden = true; searchResults.hidden = true; });
                    searchResults.appendChild(div);
                });
            } else {
                searchResults.innerHTML = '<div class="search-result">No results</div>';
            }
            searchResults.hidden = false;
        } catch {}
    }

    // Lightbox
    function openLightbox(src) { lightboxImg.src = src; lightbox.hidden = false; }

    // Helpers
    function scrollToMsg(id) {
        const el = messageList.querySelector(`[data-id="${id}"]`);
        if (el) { el.scrollIntoView({ behavior: 'smooth', block: 'center' }); el.classList.add('highlight'); setTimeout(() => el.classList.remove('highlight'), 1500); }
    }

    async function logout() {
        try { await fetch('/api/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrfToken } }); } catch {}
        sessionStorage.clear(); window.location.href = '/';
    }

    function hideEmpty() { if (emptyState && !emptyState.hidden) emptyState.hidden = true; }
    function isAtBottom() { return chatArea.scrollHeight - chatArea.scrollTop - chatArea.clientHeight < 80; }
    function scrollBottom() { requestAnimationFrame(() => { chatArea.scrollTop = chatArea.scrollHeight; }); }
    function dateSep(label) { const d = document.createElement('div'); d.className = 'date-separator'; const s = document.createElement('span'); s.textContent = label; d.appendChild(s); return d; }
    function showToast(msg, type) { if (!toast) return; toast.textContent = msg; toast.className = 'toast show ' + (type||''); clearTimeout(toast._t); toast._t = setTimeout(() => toast.className = 'toast', 3000); }
    function fmtTime(s) { return new Date(s).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }); }
    function fmtSize(b) { return b < 1024 ? b+' B' : b < 1048576 ? (b/1024).toFixed(1)+' KB' : (b/1048576).toFixed(1)+' MB'; }
    function esc(s) { return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;'); }
    function formatDateLabel(s) {
        const d = new Date(s), now = new Date();
        if (d.toDateString() === now.toDateString()) return 'Today';
        const y = new Date(now); y.setDate(y.getDate()-1);
        if (d.toDateString() === y.toDateString()) return 'Yesterday';
        return d.toLocaleDateString([], { weekday: 'long', month: 'short', day: 'numeric' });
    }
})();
