document.querySelector('#app').dataset.origin = location.origin;
new WebSocket((location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/ws');
