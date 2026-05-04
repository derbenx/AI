import {GetSpecs, GetConfig, SaveSettings, ListModels, ListClips, GetBalancedLayers, SendMessage, ClearHistory, CheckServerExecutable, StartServer, StopServer, IsServerRunning, GetImageBase64, GetFileContent} from '../wailsjs/go/main/App';
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
window.addEventListener('dragover', (e) => e.preventDefault());
window.addEventListener('drop', (e) => e.preventDefault());

EventsOn('file-dropped', (filePath) => {
    handleFilePath(filePath);
});

async function handleFilePath(filePath) {
    currentImagePath = filePath;
    const lowerPath = filePath.toLowerCase();
    const fileName = filePath.split('\\').pop().split('/').pop();

    if (lowerPath.endsWith('.jpg') || lowerPath.endsWith('.jpeg') || lowerPath.endsWith('.png') || lowerPath.endsWith('.webp')) {
        try {
            const base64Data = await GetImageBase64(filePath);
            const img = document.getElementById('image-preview');
            img.src = base64Data;
            img.style.display = 'block';
            document.getElementById('code-preview').style.display = 'none';
            document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${fileName}`;
        } catch (err) {
            console.error(err);
        }
    } else {
        try {
            const content = await GetFileContent(filePath);
            const code = document.getElementById('code-preview');
            code.textContent = content.substring(0, 5000) + (content.length > 5000 ? "\n..." : "");
            code.style.display = 'block';
            document.getElementById('image-preview').style.display = 'none';
            document.getElementById('drop-zone').querySelector('p').textContent = `Loaded: ${fileName}`;
        } catch (err) {
            console.error(err);
        }
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
        appendMessage('ai', '### ⚠️ Missing Server or Backends\n\nPlease copy **ALL files** from the [llama.cpp zip](https://github.com/ggerganov/llama.cpp/releases) into the `llama/` folder.\n\nRequired:\n- `llama-server.exe`\n- `llama.dll`\n- `ggml-cpu.dll` (and other `ggml-*.dll` files)');
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
    if (specs.cpu || specs.ram > 0) {
        document.getElementById('system-specs').style.display = 'block';
        document.getElementById('spec-cpu').textContent = specs.cpu || 'Unknown';
        document.getElementById('spec-ram').textContent = specs.ram;
        document.getElementById('spec-gpu').textContent = specs.gpu || 'None Detected';
        document.getElementById('spec-vram').textContent = specs.vram;
    }

    const config = await GetConfig();
    document.getElementById('personality-input').value = config.personality;
    document.getElementById('memory-limit').value = config.memory_limit;
    document.getElementById('remember-first').checked = config.remember_first;
    document.getElementById('debug-log').checked = config.debug_log;
    document.getElementById('gpu-layers').value = config.gpu_layers;
    document.getElementById('server-url').value = config.server_url;
    document.getElementById('server-mode').value = config.server_mode;

    const updateModeUI = () => {
        const mode = document.getElementById('server-mode').value;
        const isLocal = mode === 'local';

        document.querySelectorAll('.local-only').forEach(el => {
            el.style.display = isLocal ? 'flex' : 'none';
        });
        document.querySelectorAll('.remote-only').forEach(el => {
            el.style.display = isLocal ? 'none' : 'flex';
        });
    };
    document.getElementById('server-mode').onchange = updateModeUI;
    updateModeUI();

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
        alert('llama-server.exe or ggml-*.dll missing in llama/ folder. Copy all files from the llama.cpp zip.');
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

document.getElementById('test-connection-btn').onclick = async () => {
    const url = document.getElementById('server-url').value;
    const status = document.getElementById('connection-status');
    status.textContent = "Testing...";
    status.style.color = "white";

    try {
        const resp = await fetch(url + "/health");
        if (resp.ok) {
            status.textContent = "✅ Connected Successfully";
            status.style.color = "#44ff44";
        } else {
            status.textContent = `❌ Server returned error: ${resp.status}`;
            status.style.color = "#ff4444";
        }
    } catch (err) {
        status.textContent = `❌ Failed to connect: ${err}`;
        status.style.color = "#ff4444";
    }
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
        server_url: document.getElementById('server-url').value,
        server_mode: document.getElementById('server-mode').value,
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
