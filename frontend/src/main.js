import {GetSpecs, GetConfig, SaveSettings, ListModels, ListClips, GetBalancedLayers, SendMessage, ClearHistory, CheckServerExecutable, StartServer, StopServer, IsServerRunning} from '../wailsjs/go/main/App';
import {EventsOn, BrowserOpenURL} from '../wailsjs/runtime/runtime';

let currentImagePath = "";

// Tab switching
window.showTab = function(element, tabName) {
    document.querySelectorAll('.tab-content').forEach(tab => tab.classList.remove('active'));
    document.querySelectorAll('.tab-btn').forEach(btn => btn.classList.remove('active'));

    document.getElementById(`${tabName}-tab`).classList.add('active');
    element.classList.add('active');
}

// Drag & Drop
EventsOn('file-dropped', (filePath) => {
    handleFilePath(filePath);
});

function handleFilePath(filePath) {
    currentImagePath = filePath;
    const lowerPath = filePath.toLowerCase();

    if (lowerPath.endsWith('.jpg') || lowerPath.endsWith('.jpeg') || lowerPath.endsWith('.png') || lowerPath.endsWith('.webp')) {
        // We can't directly load local files into <img> in Wails without a custom asset handler or base64
        // For simplicity, we'll use a placeholder or just the path for now.
        // In a real app, we'd use a custom asset server or Wails.Read(filePath)
        const img = document.getElementById('image-preview');
        // Hack: Use a placeholder or assume the backend will handle it.
        img.src = "https://via.placeholder.com/300x200?text=Image+Loaded";
        img.style.display = 'block';
        document.getElementById('code-preview').style.display = 'none';
        document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${filePath.split('\\').pop().split('/').pop()}`;
    } else {
        // Treat as code
        const code = document.getElementById('code-preview');
        code.textContent = `File path: ${filePath}\n(Wails security prevents direct local file reading in browser, but backend has the path)`;
        code.style.display = 'block';
        document.getElementById('image-preview').style.display = 'none';
        document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${filePath.split('\\').pop().split('/').pop()}`;
    }
}

// Sidebar Resize
const handle = document.getElementById('resize-handle');
const sidebar = document.getElementById('preview-sidebar');
let isResizing = false;

handle.addEventListener('mousedown', (e) => {
    isResizing = true;
    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', () => {
        isResizing = false;
        document.removeEventListener('mousemove', handleMouseMove);
    });
});

function handleMouseMove(e) {
    if (!isResizing) return;
    const offsetRight = document.body.offsetWidth - e.clientX;
    if (offsetRight > 100 && offsetRight < 600) {
        sidebar.style.width = offsetRight + 'px';
    }
}

// Chat logic
const chatWindow = document.getElementById('chat-window');
const chatInput = document.getElementById('chat-input');
const sendBtn = document.getElementById('send-btn');

function appendMessage(role, content) {
    const div = document.createElement('div');
    div.className = `message ${role}`;
    div.id = role === 'ai' ? 'latest-ai-msg' : '';
    div.innerHTML = role === 'ai' ? marked.parse(content) : content;
    chatWindow.appendChild(div);
    chatWindow.scrollTop = chatWindow.scrollHeight;
    return div;
}

let currentAiMsgDiv = null;
let currentAiContent = "";

EventsOn('token', (token) => {
    if (!currentAiMsgDiv) {
        currentAiMsgDiv = appendMessage('ai', '');
    }
    currentAiContent += token;
    currentAiMsgDiv.innerHTML = marked.parse(currentAiContent);
    chatWindow.scrollTop = chatWindow.scrollHeight;
});

EventsOn('done', () => {
    currentAiMsgDiv = null;
    currentAiContent = "";
});

sendBtn.onclick = async () => {
    const text = chatInput.value.trim();
    if (!text && !currentImagePath) return;

    const hasServer = await CheckServerExecutable();
    if (!hasServer) {
        appendMessage('ai', '### ⚠️ Missing llama-server.exe\n\nPlease place `llama-server.exe` from the [llama.cpp releases](https://github.com/ggerganov/llama.cpp/releases) into the `llama/` folder to start chatting.');
        return;
    }

    appendMessage('user', text);
    chatInput.value = "";

    try {
        await SendMessage(text, currentImagePath);
    } catch (err) {
        appendMessage('ai', `Error: ${err}`);
    }
};

document.getElementById('clear-btn').onclick = async () => {
    await ClearHistory();
    chatWindow.innerHTML = "";
};

// Settings logic
async function initSettings() {
    const specs = await GetSpecs();
    document.getElementById('spec-cpu').textContent = specs.cpu;
    document.getElementById('spec-ram').textContent = specs.ram;
    document.getElementById('spec-gpu').textContent = specs.gpu;
    document.getElementById('spec-vram').textContent = specs.vram;

    const config = await GetConfig();
    document.getElementById('personality-input').value = config.personality;
    document.getElementById('memory-limit').value = config.memory_limit;
    document.getElementById('remember-first').checked = config.remember_first;
    document.getElementById('debug-log').checked = config.debug_log;
    document.getElementById('gpu-layers').value = config.gpu_layers;

    const models = await ListModels();
    const modelSelect = document.getElementById('model-select');
    models.forEach(m => {
        const opt = document.createElement('option');
        opt.value = m;
        opt.textContent = m;
        if (m === config.model_path) opt.selected = true;
        modelSelect.appendChild(opt);
    });

    const clips = await ListClips();
    const clipSelect = document.getElementById('clip-select');
    clips.forEach(c => {
        const opt = document.createElement('option');
        opt.value = c;
        opt.textContent = c;
        if (c === config.clip_path) opt.selected = true;
        clipSelect.appendChild(opt);
    });

    modelSelect.onchange = async () => {
        const balanced = await GetBalancedLayers(modelSelect.value);
        document.getElementById('balanced-recommend').textContent = balanced;
    };
    modelSelect.onchange();

    await updateServerStatus();
}

async function updateServerStatus() {
    const running = await IsServerRunning();
    const status = document.getElementById('server-status');
    status.textContent = running ? 'Running' : 'Stopped';
    status.style.color = running ? '#44ff44' : '#ff4444';
}

document.getElementById('start-server-btn').onclick = async () => {
    const hasServer = await CheckServerExecutable();
    if (!hasServer) {
        alert('llama-server.exe not found in llama/ folder');
        return;
    }
    try {
        await StartServer();
        await updateServerStatus();
    } catch (err) {
        alert(`Error: ${err}`);
    }
};

document.getElementById('stop-server-btn').onclick = async () => {
    await StopServer();
    await updateServerStatus();
};

document.getElementById('save-settings-btn').onclick = async () => {
    const config = {
        model_path: document.getElementById('model-select').value,
        clip_path: document.getElementById('clip-select').value,
        personality: document.getElementById('personality-input').value,
        memory_limit: parseInt(document.getElementById('memory-limit').value),
        remember_first: document.getElementById('remember-first').checked,
        debug_log: document.getElementById('debug-log').checked,
        gpu_layers: parseInt(document.getElementById('gpu-layers').value),
    };
    const result = await SaveSettings(config);
    alert(result);
};

// Intercept link clicks to open in external browser
document.addEventListener('click', (e) => {
    const link = e.target.closest('a');
    if (link && link.href && (link.href.startsWith('http://') || link.href.startsWith('https://'))) {
        e.preventDefault();
        BrowserOpenURL(link.href);
    }
});

initSettings();
