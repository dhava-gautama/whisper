// Sakura petals (lightweight)
(function() {
    const count = window.innerWidth < 600 ? 4 : 8;
    for (let i = 0; i < count; i++) {
        const p = document.createElement('div');
        p.className = 'petal';
        p.style.left = Math.random() * 100 + 'vw';
        p.style.animationDuration = (8 + Math.random() * 10) + 's';
        p.style.animationDelay = (Math.random() * 10) + 's';
        const size = (6 + Math.random() * 6) + 'px';
        p.style.width = size;
        p.style.height = size;
        document.body.appendChild(p);
    }
})();

document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const errorEl = document.getElementById('error');
    const btn = document.getElementById('submit-btn');
    errorEl.hidden = true;

    const password = document.getElementById('password').value;

    btn.disabled = true;
    btn.textContent = 'Signing in...';

    try {
        const res = await fetch('/api/login', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ password }),
        });

        if (res.status === 429) {
            errorEl.textContent = 'Too many attempts. Try again later.';
            errorEl.hidden = false;
            btn.disabled = false;
            btn.textContent = 'Enter';
            return;
        }

        if (!res.ok) {
            errorEl.textContent = 'Invalid password.';
            errorEl.hidden = false;
            btn.disabled = false;
            btn.textContent = 'Enter';
            document.getElementById('password').select();
            return;
        }

        const data = await res.json();
        sessionStorage.setItem('csrf_token', data.csrf_token);
        sessionStorage.setItem('user_id', data.user.id);
        sessionStorage.setItem('username', data.user.username);
        window.location.href = '/chat';
    } catch (err) {
        errorEl.textContent = 'Connection error. Please try again.';
        errorEl.hidden = false;
        btn.disabled = false;
        btn.textContent = 'Enter';
    }
});
