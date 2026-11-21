// P2P Mesh File Sharing Dashboard - Client Application
(function () {
  'use strict';

  // Application State
  const state = {
    peerID: '',
    port: 0,
    peers: [],
    shares: [],
    currentTransfer: null,
    chunkCount: 0,
    completedChunks: 0,
  };

  // DOM Elements
  const el = {
    peerIdShort: document.getElementById('peer-id-short'),
    btnCopyPeerId: document.getElementById('btn-copy-peer-id'),
    statPort: document.getElementById('stat-port'),
    statPeersCount: document.getElementById('stat-peers-count'),
    statSharesCount: document.getElementById('stat-shares-count'),
    statTransferSpeed: document.getElementById('stat-transfer-speed'),
    
    // Transfers & Chunk Grid
    transferFileName: document.getElementById('transfer-file-name'),
    transferFileSize: document.getElementById('transfer-file-size'),
    transferChunksRatio: document.getElementById('transfer-chunks-ratio'),
    transferEta: document.getElementById('transfer-eta'),
    transferStatusText: document.getElementById('transfer-status-text'),
    transferPercentText: document.getElementById('transfer-percent-text'),
    transferProgressFill: document.getElementById('transfer-progress-fill'),
    chunkGrid: document.getElementById('chunk-grid'),
    btnPause: document.getElementById('btn-pause-transfer'),
    btnResume: document.getElementById('btn-resume-transfer'),

    // Forms
    formDownload: document.getElementById('form-download'),
    inputDownloadFid: document.getElementById('input-download-fid'),
    selectDownloadPeer: document.getElementById('select-download-peer'),
    sharesTableBody: document.getElementById('shares-table-body'),
    peersTableBody: document.getElementById('peers-table-body'),

    // Dropzone & Share
    dropzone: document.getElementById('file-dropzone'),
    fileInput: document.getElementById('file-input'),
    formSharePath: document.getElementById('form-share-path'),
    inputSharePath: document.getElementById('input-share-path'),

    // Add Peer Modal
    modalAddPeer: document.getElementById('modal-add-peer'),
    btnOpenAddPeer: document.getElementById('btn-open-add-peer'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    formAddPeer: document.getElementById('form-add-peer'),
    inputPeerEndpoint: document.getElementById('input-peer-endpoint'),

    // Toasts
    toastContainer: document.getElementById('toast-container'),
  };

  // Utilities
  function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
  }

  function showToast(message, type = 'success') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    el.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      toast.style.transition = 'all 200ms ease';
      setTimeout(() => toast.remove(), 200);
    }, 3500);
  }

  function copyToClipboard(text, label = 'Copied to clipboard!') {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text)
        .then(() => showToast(label))
        .catch(() => showToast('Failed to copy', 'error'));
    } else {
      // Fallback
      const ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
      showToast(label);
    }
  }

  // API Client Calls
  async function fetchStatus() {
    try {
      const res = await fetch('/api/status');
      if (!res.ok) return;
      const data = await res.json();
      state.peerID = data.peer_id;
      state.port = data.port;

      if (el.peerIdShort) {
        el.peerIdShort.textContent = data.peer_id.substring(0, 12) + '...';
      }
      if (el.statPort) {
        el.statPort.textContent = data.port;
      }
    } catch (err) {
      console.warn('Status poll failed:', err);
    }
  }

  async function fetchPeers() {
    try {
      const res = await fetch('/api/peers');
      if (!res.ok) return;
      const peers = await res.json();
      state.peers = peers;

      if (el.statPeersCount) {
        el.statPeersCount.textContent = peers.length;
      }

      // Update peer select dropdown
      if (el.selectDownloadPeer) {
        const currentVal = el.selectDownloadPeer.value;
        el.selectDownloadPeer.innerHTML = '<option value="">-- Choose Discovered Peer or Enter Endpoint --</option>';
        peers.forEach(p => {
          const opt = document.createElement('option');
          opt.value = p.endpoint;
          opt.textContent = `${p.endpoint} (${p.source})`;
          el.selectDownloadPeer.appendChild(opt);
        });
        if (currentVal) el.selectDownloadPeer.value = currentVal;
      }

      // Update peer table
      if (el.peersTableBody) {
        if (peers.length === 0) {
          el.peersTableBody.innerHTML = `
            <tr>
              <td colspan="3" style="text-align:center; color: var(--color-text-muted); padding: 1.5rem;">
                No peers discovered yet. Click "+ Add Peer" to connect manually.
              </td>
            </tr>`;
        } else {
          el.peersTableBody.innerHTML = peers.map(p => `
            <tr>
              <td class="mono">${p.endpoint}</td>
              <td>${p.source}</td>
              <td><span class="status-badge active"><span class="status-dot"></span> ${p.status}</span></td>
            </tr>
          `).join('');
        }
      }
    } catch (err) {
      console.warn('Peers fetch failed:', err);
    }
  }

  async function fetchShares() {
    try {
      const res = await fetch('/api/shares');
      if (!res.ok) return;
      const shares = await res.json();
      state.shares = shares;

      if (el.statSharesCount) {
        el.statSharesCount.textContent = shares.length;
      }

      if (el.sharesTableBody) {
        if (shares.length === 0) {
          el.sharesTableBody.innerHTML = `
            <tr>
              <td colspan="5" style="text-align:center; color: var(--color-text-muted); padding: 1.5rem;">
                No files currently shared. Publish a file using the form.
              </td>
            </tr>`;
        } else {
          el.sharesTableBody.innerHTML = shares.map(s => `
            <tr>
              <td style="font-weight: 500; color: var(--color-text-primary);">${s.file_name}</td>
              <td>${formatBytes(s.total_bytes)}</td>
              <td>${s.chunk_count}</td>
              <td class="mono" style="font-size:0.75rem;">${s.file_id.substring(0, 16)}...</td>
              <td>
                <button class="btn btn-secondary" style="padding: 0.25rem 0.5rem; font-size: 0.75rem;" onclick="window.copyFileId('${s.file_id}')">
                  Copy ID
                </button>
              </td>
            </tr>
          `).join('');
        }
      }
    } catch (err) {
      console.warn('Shares fetch failed:', err);
    }
  }

  window.copyFileId = function (fid) {
    copyToClipboard(fid, 'File ID copied to clipboard!');
  };

  // Chunk Grid Visualizer
  function initializeChunkGrid(totalChunks) {
    state.chunkCount = totalChunks;
    state.completedChunks = 0;
    el.chunkGrid.innerHTML = '';

    for (let i = 0; i < totalChunks; i++) {
      const block = document.createElement('div');
      block.className = 'chunk-block';
      block.id = `chunk-${i}`;
      block.title = `Chunk #${i} - Status: Pending`;
      el.chunkGrid.appendChild(block);
    }

    updateProgress(0, totalChunks);
  }

  function setChunkState(index, status) {
    const block = document.getElementById(`chunk-${index}`);
    if (!block) return;

    block.classList.remove('fetching', 'verified', 'corrupt');
    if (status !== 'pending') {
      block.classList.add(status);
    }
    block.title = `Chunk #${index} - Status: ${status.toUpperCase()}`;
  }

  function updateProgress(completed, total, speedMbps = 0) {
    const pct = total > 0 ? Math.round((completed / total) * 100) : 0;
    if (el.transferPercentText) el.transferPercentText.textContent = `${pct}%`;
    if (el.transferChunksRatio) el.transferChunksRatio.textContent = `${completed} / ${total} Chunks Verified`;
    if (el.transferProgressFill) {
      el.transferProgressFill.style.width = `${pct}%`;
      if (pct === 100) {
        el.transferProgressFill.classList.add('success');
      } else {
        el.transferProgressFill.classList.remove('success');
      }
    }
    if (el.statTransferSpeed && speedMbps > 0) {
      el.statTransferSpeed.textContent = `${speedMbps.toFixed(1)} MB/s`;
    }
  }

  // Server-Sent Events (SSE) Telemetry
  function initSSE() {
    const sse = new EventSource('/api/events');

    sse.onmessage = function (e) {
      try {
        const data = JSON.parse(e.data);
        handleTelemetryEvent(data);
      } catch (err) {
        console.warn('Malformed SSE event:', err);
      }
    };

    sse.onerror = function () {
      console.warn('SSE connection disconnected. Reconnecting automatically...');
    };
  }

  function handleTelemetryEvent(ev) {
    switch (ev.type) {
      case 'chunk_verified':
        if (ev.index !== undefined) {
          setChunkState(ev.index, 'verified');
          state.completedChunks = ev.completed_chunks || (state.completedChunks + 1);
          updateProgress(state.completedChunks, ev.total_chunks || state.chunkCount, ev.speed_mbps);
        }
        break;

      case 'transfer_state':
        if (el.transferStatusText) {
          el.transferStatusText.textContent = `Status: ${ev.state}`;
        }
        if (ev.state === 'Completed') {
          showToast(`File download completed successfully!`, 'success');
          if (el.btnPause) el.btnPause.style.display = 'none';
          if (el.btnResume) el.btnResume.style.display = 'none';
          if (el.statTransferSpeed) el.statTransferSpeed.textContent = '0.0 MB/s';
        } else if (ev.state === 'Active') {
          if (el.btnPause) el.btnPause.style.display = 'inline-flex';
          if (el.btnResume) el.btnResume.style.display = 'none';
        } else if (ev.state === 'Paused' || ev.state === 'Interrupted') {
          if (el.btnPause) el.btnPause.style.display = 'none';
          if (el.btnResume) el.btnResume.style.display = 'inline-flex';
          if (el.statTransferSpeed) el.statTransferSpeed.textContent = '0.0 MB/s';
        } else if (ev.state === 'Failed') {
          showToast(`Transfer failed!`, 'error');
        }
        break;

      case 'peer_update':
        fetchPeers();
        break;
    }
  }

  // Event Listeners Setup
  function initEventListeners() {
    // Copy Peer ID
    if (el.btnCopyPeerId) {
      el.btnCopyPeerId.addEventListener('click', () => {
        if (state.peerID) copyToClipboard(state.peerID, 'Node Peer ID copied to clipboard!');
      });
    }

    // Modal Add Peer
    if (el.btnOpenAddPeer && el.modalAddPeer) {
      el.btnOpenAddPeer.addEventListener('click', () => el.modalAddPeer.showModal());
    }
    if (el.btnCloseModal && el.modalAddPeer) {
      el.btnCloseModal.addEventListener('click', () => el.modalAddPeer.close());
    }
    if (el.formAddPeer) {
      el.formAddPeer.addEventListener('submit', async (e) => {
        e.preventDefault();
        const endpoint = el.inputPeerEndpoint.value.trim();
        if (!endpoint) return;

        try {
          const res = await fetch('/api/peers', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ endpoint }),
          });
          if (res.ok) {
            showToast(`Connected to peer: ${endpoint}`);
            el.inputPeerEndpoint.value = '';
            el.modalAddPeer.close();
            fetchPeers();
          } else {
            showToast('Failed to register peer', 'error');
          }
        } catch (err) {
          showToast('Network error adding peer', 'error');
        }
      });
    }

    // Dropzone & Path Publish
    if (el.dropzone) {
      el.dropzone.addEventListener('click', () => el.fileInput.click());
      el.dropzone.addEventListener('dragover', (e) => {
        e.preventDefault();
        el.dropzone.classList.add('drag-over');
      });
      el.dropzone.addEventListener('dragleave', () => el.dropzone.classList.remove('drag-over'));
      el.dropzone.addEventListener('drop', (e) => {
        e.preventDefault();
        el.dropzone.classList.remove('drag-over');
        if (e.dataTransfer.files.length > 0) {
          const file = e.dataTransfer.files[0];
          // For local desktop file sharing, prompt or use name
          showToast(`Selected file: ${file.name}. Enter local path below to publish.`, 'success');
          if (el.inputSharePath) el.inputSharePath.value = file.name;
        }
      });
    }

    if (el.formSharePath) {
      el.formSharePath.addEventListener('submit', async (e) => {
        e.preventDefault();
        const filePath = el.inputSharePath.value.trim();
        if (!filePath) return;

        try {
          const res = await fetch('/api/share', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ file_path: filePath }),
          });
          const data = await res.json();
          if (res.ok) {
            showToast(`Published ${data.file_name}! (ID: ${data.file_id.substring(0, 12)}...)`);
            el.inputSharePath.value = '';
            fetchShares();
          } else {
            showToast(data.error || 'Failed to publish file', 'error');
          }
        } catch (err) {
          showToast('Network error publishing file', 'error');
        }
      });
    }

    // Download Form
    if (el.formDownload) {
      el.formDownload.addEventListener('submit', async (e) => {
        e.preventDefault();
        const fileId = el.inputDownloadFid.value.trim();
        let peer = el.selectDownloadPeer.value.trim();

        if (!fileId || fileId.length !== 64) {
          showToast('Valid 64-character SHA-256 File ID is required', 'error');
          return;
        }

        if (!peer && state.peers.length > 0) {
          peer = state.peers[0].endpoint;
        }

        if (!peer) {
          showToast('Please select or specify a remote peer endpoint', 'error');
          return;
        }

        // Initialize UI for new transfer
        state.currentTransfer = fileId;
        el.transferFileName.textContent = `File: ${fileId.substring(0, 16)}...`;
        el.transferStatusText.textContent = 'Status: Connecting...';
        initializeChunkGrid(32); // Initial estimate; updated on manifest response

        try {
          const res = await fetch('/api/download', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ file_id: fileId, peer: peer }),
          });
          if (res.ok) {
            showToast(`Download initiated from ${peer}!`);
            if (el.btnPause) el.btnPause.style.display = 'inline-flex';
          } else {
            const errData = await res.json();
            showToast(errData.error || 'Download failed to initiate', 'error');
          }
        } catch (err) {
          showToast('Network error starting download', 'error');
        }
      });
    }

    // Pause & Resume Buttons
    if (el.btnPause) {
      el.btnPause.addEventListener('click', async () => {
        if (!state.currentTransfer) return;
        try {
          await fetch(`/api/transfers/${state.currentTransfer}/pause`, { method: 'POST' });
          showToast('Transfer paused');
        } catch (err) {
          showToast('Error pausing transfer', 'error');
        }
      });
    }

    if (el.btnResume) {
      el.btnResume.addEventListener('click', async () => {
        if (!state.currentTransfer) return;
        try {
          await fetch(`/api/transfers/${state.currentTransfer}/resume`, { method: 'POST' });
          showToast('Transfer resumed');
        } catch (err) {
          showToast('Error resuming transfer', 'error');
        }
      });
    }
  }

  // Init
  function init() {
    initEventListeners();
    initSSE();
    fetchStatus();
    fetchPeers();
    fetchShares();

    // Periodic poll for status & peer table refreshes
    setInterval(fetchStatus, 5000);
    setInterval(fetchPeers, 3000);
    setInterval(fetchShares, 5000);
  }

  document.addEventListener('DOMContentLoaded', init);
})();
