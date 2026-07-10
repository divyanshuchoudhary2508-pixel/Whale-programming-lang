// Whale VSCode Extension
// Spawns wh-lsp as a child process and connects to it via the Language Client.
// The LanguageClient handles all JSON-RPC framing automatically.

const { LanguageClient, TransportKind } = require('vscode-languageclient/node');
const path = require('path');
const vscode = require('vscode');

let client;

/**
 * Called by VSCode when the extension is activated (on any .wh file).
 * @param {vscode.ExtensionContext} context
 */
async function activate(context) {
  // Path to the wh-lsp binary. We expect it to be on PATH or in the same
  // directory as the extension. Users can override via setting.
  const config = vscode.workspace.getConfiguration('whale');
  const serverPath = config.get('lsp.serverPath', 'wh-lsp');

  const serverOptions = {
    command: serverPath,
    transport: TransportKind.stdio,
  };

  const clientOptions = {
    // Activate for .wh files
    documentSelector: [{ scheme: 'file', language: 'whale' }],
    synchronize: {
      fileEvents: vscode.workspace.createFileSystemWatcher('**/*.wh'),
    },
  };

  client = new LanguageClient(
    'whale-lsp',
    'Whale Language Server',
    serverOptions,
    clientOptions
  );

  // Start the client. This launches wh-lsp and begins listening.
  await client.start();

  // Register the debug adapter factory
  const factory = new WhaleDebugAdapterFactory();
  context.subscriptions.push(vscode.debug.registerDebugAdapterDescriptorFactory('whale', factory));
  context.subscriptions.push(factory);

  vscode.window.showInformationMessage('🐋 Whale Language Server & Debugger started!');
}

class WhaleDebugAdapterFactory {
  createDebugAdapterDescriptor(session, executable) {
    const config = vscode.workspace.getConfiguration('whale');
    const dapPath = config.get('dap.serverPath', 'wh-dap');
    return new vscode.DebugAdapterExecutable(dapPath);
  }
  dispose() {}
}

/**
 * Called when the extension is deactivated (VSCode closes or extension disabled).
 */
async function deactivate() {
  if (client) {
    await client.stop();
  }
}

module.exports = { activate, deactivate };
