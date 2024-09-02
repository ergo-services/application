const socket = new WebSocket('ws://localhost:9911/ws'); // Replace with your backend WebSocket URL

function displayReceivedMessage(message) {
    const receivedMessageTextarea = document.getElementById('receivedMessage');
    receivedMessageTextarea.value += JSON.stringify(message, null, 2);
	receivedMessageTextarea.scrollTop = receivedMessageTextarea.scrollHeight;
}

function sendMessage(message) {
    socket.send(message);
}

document.getElementById('sendCustom').addEventListener('click', () => {
    const message = document.getElementById('message').value;
    sendMessage(message);
});

document.getElementById('connectButton').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "connect"
    "Name": // node name.
}`
});

document.getElementById('subscribeNetwork').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "network"
}`
});

document.getElementById('subscribeLog').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "log"
}`
});

document.getElementById('subscribeConnection').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "remote_node",
    "Args":
        {"Name": // remote node name}
}`
});


document.getElementById('subscribeProcessList').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "process_list",
    "Args":
      {"start":1003, "limit":3}
}`
});

document.getElementById('subscribeProcess').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "process",
    "Args":
      {"PID":"<4AD066C0.0.1006>"}
}`
});

document.getElementById('subscribeProcessState').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "process_state",
    "Args":
      {"PID":"<4AD066C0.0.1006>"}
}`
});

document.getElementById('subscribeMeta').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "meta",
    "Args":
      {"ID":"Alias#<4AD066C0.94927.24031.0>"}
}`
});

document.getElementById('subscribeMetaState').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "subscribe",
    "Name": "meta_state",
    "Args":
      {"ID":"Alias#<4AD066C0.94927.24031.0>"}
}`
});

document.getElementById('unsubscribe').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "unsubscribe",
    "Name": "Event#<4AD066C0:'inspect_network_event'>"
}`
});

document.getElementById('doSend').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "send",
    "Args":
      {"PID":"<4AD066C0.0.1006>", "Message": "example message"}
}`
});

document.getElementById('doSendMeta').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "send_meta",
    "Args":
      {"ID":"Alias#<4AD066C0.94927.24031.0>", "Message": "example message"}
}`
});

document.getElementById('doSendExit').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "send_exit",
    "Args":
      {"PID":"<4AD066C0.0.1006>", "Reason": "normal"}
}`
});

document.getElementById('doSendExitMeta').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "send_exit_meta",
    "Args":
      {"ID":"Alias#<4AD066C0.94927.24031.0>", "Reason": "normal"}
}`
});

document.getElementById('doKill').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "kill",
    "Args":
      {"PID":"<4AD066C0.0.1006>"}
}`
});

document.getElementById('doSetLogLevel').addEventListener('click', () => {
    const messageArea = document.getElementById('message');
	messageArea.value = `{
    "CID": "`+self.crypto.randomUUID()+`",
    "Command": "do",
    "Name": "set_log_level",
    "Args":
      {"Level":"info", "PID":"<4AD066C0.0.1006>"}
	  // or {"ID":"Alias#<4AD066C0.94927.24031.0>"}
	  // or empty to set log level for node
}`
});

document.getElementById('clearButton').addEventListener('click', () => {
    const recvTextarea = document.getElementById('receivedMessage');
	recvTextarea.value = ''
});

socket.addEventListener('message', event => {
    const data = JSON.parse(event.data);
    displayReceivedMessage(data);
});

