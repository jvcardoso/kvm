![luckfox](https://github.com/LuckfoxTECH/luckfox-pico/assets/144299491/cec5c4a5-22b9-4a9a-abb1-704b11651e88)

**[Português (BR)](#português-br)** | **[English](#english)** | **[中文](./README_CN.md)**

---

<a id="português-br"></a>

# Luckfox PicoKVM (PT-BR)

Luckfox PicoKVM é uma ferramenta leve de operação e manutenção IP KVM baseada no desenvolvimento secundário do [JetKVM](https://jetkvm.com/). Permite acesso remoto ao vídeo do dispositivo-alvo e emulação de entrada HID (teclado, mouse, touchscreen) e mídia virtual pela rede para administração sem contato de placas de desenvolvimento, PCs e servidores.

* **Fork & Melhorias**: Juliano V. Cardoso ([github.com/jvcardoso/kvm](https://github.com/jvcardoso/kvm))
* **Projeto Original Base**: Luckfox PicoKVM ([github.com/LuckfoxTECH/kvm](https://github.com/LuckfoxTECH/kvm)) • [JetKVM](https://jetkvm.com/)

## Funcionalidades Principais
* **Suporte a cartão Micro SD**: configurações de boot ou expansão de armazenamento para imagens ISO/IMG
* **USB Multifunção (ConfigFS)**: emulação combinada de Teclado/Mouse HID, Mass Storage, placa de som USB (UAC1) ou dispositivo MTP com upload/download HTTP
* **Controle de porta serial**: terminal serial integrado para depuração do equipamento controlado
* **Controle de nível IO / ATX**: acionamento de alimentação e GPIOs de expansão
* **Tela sensível ao toque de 1,54"**: exibição de IP, estado de conexão e telemetria do sistema
* **Acesso remoto WebRTC e VPN**: gerenciamento de baixa latência via WebRTC, proxy reverso FRP, Tailscale, ZeroTier, EasyTier e WireGuard

## Melhorias Implementadas sobre o Projeto Original

Esta versão (`AppVersion: 0.1.6` / `SystemVersion: 0.1.9`) inclui um conjunto abrangente de correções de segurança, estabilidade, concorrência, USB ConfigFS, mídia virtual, internacionalização (`pt-BR` / ABNT2), usabilidade móvel e suporte a múltiplos repositórios OTA.

### 1. Segurança, Autenticação e Controle de Acesso
* **Proteção de endpoints administrativos e CORS**:
  * Remoção de `Access-Control-Allow-Origin: *` indiscriminado e validação estrita de `Origin` nos WebSockets (`/webrtc/signaling/client`, `/terminal/ws` e serial), bloqueando requisições cross-site (CSWSH).
  * Exigência de sessão autenticada (`protectedRoutes`) para rotas críticas como `/ota/upload-local-pkg` e endpoints de gerenciamento.
* **Cookie de sessão e TLS**:
  * Cookies de autenticação configurados com `HttpOnly` e `SameSite=Lax`, ativando `Secure` automaticamente quando a requisição chega via HTTPS ou cabeçalho TLS.
  * Validação criptográfica da correspondência entre certificado e chave privada TLS antes de persistir configurações HTTPS customizadas.
* **Isolamento e validação de caminhos (Path Traversal)**:
  * Sanitização rigorosa de nomes de arquivo e validação de caminhos reais (`filepath.EvalSymlinks`) no gerenciador de arquivos/MTP, upload de imagens de mídia virtual e extração de pacotes `.tar.gz` das ferramentas de rede/VPN (bloqueando *zip-slip*, symlinks externos, hardlinks e dispositivos especiais).
* **Execução segura de comandos de sistema**:
  * Eliminação de interpolação em shell (`sh -c`) no módulo de rede e Wi-Fi (`network.go`), substituindo por chamadas diretas com argumentos separados (`exec.Command`) e validação estrita de nomes de interface, SSID e PSK.
* **Servidor MCP autenticado e restrito a loopback**:
  * Bind padrão restrito a `127.0.0.1` quando nenhum token é definido e exigência de autenticação Bearer constante (`subtle.ConstantTimeCompare`) quando exposto na rede.

### 2. Estabilidade, Concorrência e Persistência (Backend Go)
* **Gravação atômica e resiliente de configuração (`config.go`)**:
  * Escrita via arquivo temporário no mesmo diretório seguida de `Sync()`, `Close()` e `os.Rename()` atômico, preservando um backup `.bak` válido e evitando corrupção de `/userdata/kvm_config.json` em quedas de energia.
  * Migração segura de configurações legadas ou nulas sem sobrescrever credenciais nem quebrar quando o cartão SD não está presente.
* **Sincronização de sessões WebRTC e timers (`webrtc.go`, `main.go`, `timesync.go`)**:
  * Proteção concorrente de `currentSession` com mutex dedicado e troca atômica de sessão ao desconectar ou substituir clientes.
  * Uso de `atomic.Bool` no estado de sincronização NTP (`timeSyncDone`) e limpeza de goroutines órfãs no coletor de estatísticas de rede.
* **Prevenção de deadlocks e corridas em JSON-RPC e OTA (`jsonrpc.go`, `ota.go`)**:
  * Liberação de locks de configuração antes de chamadas de subsistema (display, USB, áudio, rede) e serialização completa do fluxo de atualização OTA com verificação de hash SHA-256 antes da substituição do binário.
* **Deadlines e recuperação dos processos nativos (`native.go`, `native_display.go`, `native_audio.go`, `native_vpn.go`)**:
  * Aplicação de timeouts de leitura/escrita nos sockets Unix de vídeo, display, áudio e VPN, evitando bloqueio indefinido caso um processo auxiliar trave, com reconexão automática supervisionada.
* **Ativação atômica de ferramentas VPN (`tools.go`, `vpn_tool_exchange_*.go`)**:
  * Troca atômica de diretórios (`RENAME_EXCHANGE` no Linux) durante a instalação/atualização de binários de VPN (Tailscale, ZeroTier, EasyTier, WireGuard) com rollback automático em caso de falha.

### 3. USB Gadget e Recuperação HID (`internal/usbgadget`)
* **Correção do erro `failed to start kvm: -19` (`-ENODEV`) no controlador DWC3**:
  * O controlador USB do RK3506/RV1106 possui limite de endpoints de hardware. Quando `Audio` (`uac1.usb0`) ou `MTP` está habilitado, a função extra `hid.usb3` (pen/touchscreen) agora é automaticamente desabilitada em `HasPenTouch()`, garantindo que teclado (`hid.usb0`), mouse absoluto (`hid.usb1`), mouse relativo (`hid.usb2`) e áudio/mass-storage subam simultaneamente sem exaurir endpoints.
* **Rebind limpo do ConfigFS**:
  * `configureUsbGadget()` executa `ensureGadgetUnbound()` dentro do ciclo `withHIDRebind`, fechando descritores `/dev/hidg*` abertos antes de alterar symlinks do ConfigFS e religando o UDC (`ffb00000.usb`) de forma determinística.

### 4. Mídia Virtual: NBD, WebRTC Disk e HTTP Range (`remote_mount.go`, `block_device.go`, `fuse.go`)
* **Montagem remota via WebRTC e HTTP**:
  * Implementação do hook `useWebRTCDisk` no frontend e validação de limites de offset/tamanho no canal `disk` do backend.
  * Suporte completo ao ciclo `mountWithHTTP` com verificação `HEAD`/Range, cache em memória com substituição de blocos (evitando crescimento ilimitado de RAM) e desmontagem limpa antes de trocar de mídia.
* **Dispositivo NBD resiliente**:
  * Abertura do `/dev/nbd0` com `O_RDWR`, desconexão explícita (`NBD_CLEAR_SOCK` / `NBD_DISCONNECT`) no encerramento e espera ativa pela prontidão do bloco antes de vinculá-lo ao gadget USB Mass Storage.

### 5. Internacionalização (`pt-BR`) e Teclado Brasileiro ABNT2
* **Tradução completa para Português do Brasil ([pt.json](./ui/src/locales/pt.json))**:
  * Interface inteiramente traduzida para `pt-BR` (menus, configurações, energia/ATX, mídia virtual, rede, VPN, áudio, vídeo, terminal, diagnóstico e instruções de gestos).
* **Layout de teclado ABNT2 (`pt_BR`) ([pt_BR.ts](./ui/src/keyboardLayouts/pt_BR.ts))**:
  * Mapeamento HID completo para o padrão brasileiro ABNT2, incluindo `ç`/`Ç` (`Semicolon`), barra/interrogação (`IntlRo` e `AltGr+Q`/`AltGr+W`), barra invertida/pipe (`IntlBackslash`), acentos/dead keys (`´`, `` ` ``, `~`, `^`, `¨`) e combinações com `AltGr`.

### 6. Experiência Mobile e Controle por Toque (Android / iOS)
* **Visualização em tela cheia sem cortes ([DesktopMobile.tsx](./ui/src/layout/core/desktop/DesktopMobile.tsx), [useTouchZoom.ts](./ui/src/layout/core/desktop/hooks/useTouchZoom.ts))**:
  * Vídeo com `object-fit: contain` em retrato e paisagem, exibindo 100% da área do monitor remoto sem cortar barras do sistema operacional.
  * Limite de pan/zoom restrito à área real da imagem (desconsiderando as faixas de *letterbox*) e recalculado automaticamente via `ResizeObserver` ao girar a tela ou abrir o teclado.
* **Teclado virtual móvel com fila serializada ([hidTapQueue.ts](./ui/src/utils/hidTapQueue.ts), [useKeyboardTaps.ts](./ui/src/hooks/useKeyboardTaps.ts), [VirtualKeyboard.tsx](./ui/src/components/VirtualKeyboard.tsx))**:
  * Substituição dos timers concorrentes de 100 ms por uma fila serializada de pares `press` (35 ms) + `release` (15 ms), preservando letras repetidas em digitação rápida e garantindo a liberação da tecla mesmo ao fechar o teclado, perder foco ou trocar de sessão.
  * Teclado móvel compacto de 4 linhas com suporte completo ao layout ABNT2 (`ç`, `AltGr`, `\`, acentos e símbolos brasileiros) e abertura com 1 toque na barra inferior.
* **Gestos de mouse precisos com e sem zoom ([useTouchGestures.ts](./ui/src/layout/core/desktop/hooks/useTouchGestures.ts), [useMouseEvents.ts](./ui/src/layout/core/desktop/hooks/useMouseEvents.ts))**:
  * **1 dedo**: move o cursor sem arrastar involuntariamente (`buttons = 0`), inclusive quando a imagem está ampliada; um toque rápido gera clique esquerdo ao soltar.
  * **Toque longo (1 dedo)**: envia apenas clique direito (`BUTTON_RIGHT`), sem disparar clique esquerdo antes.
  * **Duplo toque + arraste**: mantém o botão esquerdo pressionado no segundo toque para selecionar texto ou mover janelas.
  * **2 dedos**: pinça para zoom e pan da imagem (ou rolagem remota quando o modo de rolagem está ativo), cancelando imediatamente qualquer clique pendente do primeiro dedo.
  * **Modo relativo em telas touch**: acumula frações subpixel e divide movimentos grandes (`> 127 px`) em múltiplos relatórios HID de 8 bits sem depender de Pointer Lock.

### 7. Créditos e Dupla Fonte de Atualização GitHub (`VersionContent.tsx`, `ota.go`)
* Exibição de créditos e links para os dois repositórios diretamente em **Settings → Version**.
* Seleção de fonte de atualização OTA entre **`github (jvcardoso)`** (repositório deste fork com fallback para a Luckfox) e **`github (LuckfoxTECH)`** (repositório oficial original), além de `custom` e `local`.

## Compilação (PT-BR)
* Compilar apenas o frontend:
  ```bash
  make frontend
  ```
* Compilar apenas o backend (desenvolvimento):
  ```bash
  make build_dev
  ```
* Compilação completa de release (frontend + backend ARMv7):
  ```bash
  make build_release
  ```
* O binário gerado em **`bin/kvm_app`** pode ser enviado ao Luckfox PicoKVM via pacote OTA local (`/ota/upload-local-pkg`) ou via SSH/MTP substituindo **`/userdata/picokvm/bin/kvm_app`**.

---

<a id="english"></a>

# Luckfox PicoKVM (English)

Luckfox PicoKVM is a lightweight IP KVM operation and maintenance tool based on a secondary development of [JetKVM](https://jetkvm.com/). It allows remote access to a target device's display and emulates HID input (keyboard, mouse, touchscreen) and virtual media over the network, enabling contactless operation and management of development boards, PCs, and servers.

* **Fork & Improvements**: Juliano V. Cardoso ([github.com/jvcardoso/kvm](https://github.com/jvcardoso/kvm))
* **Original Base Project**: Luckfox PicoKVM ([github.com/LuckfoxTECH/kvm](https://github.com/LuckfoxTECH/kvm)) • [JetKVM](https://jetkvm.com/)

## Key Features
* **Micro SD card support**: Used for software boot settings or storage expansion for ISO/IMG images
* **USB multifunction configuration (ConfigFS)**: Emulates combined HID Keyboard/Mouse, Mass Storage, USB sound card (UAC1), or MTP device with HTTP file upload/download
* **Serial port control**: Built-in serial terminal for control and debugging of the target device
* **IO level / ATX control**: Controls expansion GPIO output and ATX power switching
* **1.54-inch touchscreen**: Displays IP address, connection status, and system runtime telemetry
* **WebRTC & VPN remote access**: Low-latency remote management via WebRTC, FRP reverse proxy, Tailscale, ZeroTier, EasyTier, and WireGuard

## Improvements Added on Top of the Original Project

This build (`AppVersion: 0.1.6` / `SystemVersion: 0.1.9`) introduces comprehensive fixes and enhancements across security, backend concurrency, USB ConfigFS, virtual media, internationalization (`pt-BR` / ABNT2), mobile touch UX, and dual GitHub OTA update sources.

### 1. Security, Authentication & Access Control
* **Administrative endpoint & CORS protection**:
  * Removed wildcard `Access-Control-Allow-Origin: *` and enforced strict `Origin` validation on WebSockets (`/webrtc/signaling/client`, `/terminal/ws`, and serial), preventing Cross-Site WebSocket Hijacking (CSWSH).
  * Required authenticated sessions (`protectedRoutes`) on sensitive management routes including `/ota/upload-local-pkg`.
* **Session cookies & TLS validation**:
  * Configured authentication cookies with `HttpOnly` and `SameSite=Lax`, enabling `Secure` automatically when served over HTTPS/TLS.
  * Added cryptographic matching verification between custom TLS certificates and private keys before saving HTTPS settings.
* **Path Traversal prevention & archive sanitization**:
  * Strict filename sanitization and real-path verification (`filepath.EvalSymlinks`) across the file manager/MTP, virtual media uploads, and `.tar.gz` extraction for VPN/network tools (blocking zip-slip, external symlinks, hardlinks, and device nodes).
* **Safe system command execution**:
  * Eliminated shell interpolation (`sh -c`) in network and Wi-Fi management (`network.go`), replacing it with direct argument execution (`exec.Command`) and strict interface/SSID/PSK validation.
* **Authenticated loopback-first MCP server**:
  * Restricted default MCP server binding to `127.0.0.1` when no token is configured and enforced constant-time Bearer token comparison (`subtle.ConstantTimeCompare`) when exposed on the network.

### 2. Backend Stability, Concurrency & Persistence (Go)
* **Atomic & resilient configuration persistence (`config.go`)**:
  * Writes configuration via a same-directory temporary file followed by `Sync()`, `Close()`, and atomic `os.Rename()`, preserving a valid `.bak` backup and preventing `/userdata/kvm_config.json` corruption on power loss.
  * Safe migration of legacy or null configuration files without overwriting credentials or failing when no SD card is inserted.
* **WebRTC session & timer synchronization (`webrtc.go`, `main.go`, `timesync.go`)**:
  * Protected `currentSession` with a dedicated mutex and atomic session handoff on client disconnect or replacement.
  * Used `atomic.Bool` for NTP synchronization state (`timeSyncDone`) and cleaned up orphaned goroutines in the network statistics collector.
* **Deadlock & race condition prevention in JSON-RPC and OTA (`jsonrpc.go`, `ota.go`)**:
  * Released configuration locks prior to subsystem calls (display, USB, audio, network) and serialized the OTA update flow with SHA-256 verification before binary replacement.
* **Native process socket deadlines & recovery (`native.go`, `native_display.go`, `native_audio.go`, `native_vpn.go`)**:
  * Added read/write deadlines on Unix domain sockets for video, display, audio, and VPN helper processes to prevent indefinite stalls, paired with supervised auto-reconnection.
* **Atomic VPN tool activation (`tools.go`, `vpn_tool_exchange_*.go`)**:
  * Atomic directory exchange (`RENAME_EXCHANGE` on Linux) when installing or updating VPN binaries (Tailscale, ZeroTier, EasyTier, WireGuard) with automatic rollback on failure.

### 3. USB Gadget & HID Recovery (`internal/usbgadget`)
* **Fixed DWC3 `failed to start kvm: -19` (`-ENODEV`) endpoint exhaustion**:
  * The RK3506/RV1106 USB controller has a hardware endpoint limit. When `Audio` (`uac1.usb0`) or `MTP` is enabled, the extra `hid.usb3` (pen/touchscreen) function is now automatically disabled in `HasPenTouch()`, allowing keyboard (`hid.usb0`), absolute mouse (`hid.usb1`), relative mouse (`hid.usb2`), and audio/mass-storage to bind simultaneously without exhausting hardware endpoints.
* **Clean ConfigFS rebind lifecycle**:
  * `configureUsbGadget()` executes `ensureGadgetUnbound()` inside `withHIDRebind`, closing open `/dev/hidg*` descriptors before modifying ConfigFS symlinks and rebinding the UDC (`ffb00000.usb`) deterministically.

### 4. Virtual Media: NBD, WebRTC Disk & HTTP Range (`remote_mount.go`, `block_device.go`, `fuse.go`)
* **WebRTC & HTTP remote media mounting**:
  * Implemented the `useWebRTCDisk` frontend hook and backend offset/length bounds checking on the `disk` DataChannel.
  * Complete `mountWithHTTP` lifecycle with `HEAD`/Range validation, bounded in-memory block cache (preventing unbounded RAM growth), and clean unmounting before switching media.
* **Resilient NBD block device handling**:
  * Opens `/dev/nbd0` with `O_RDWR`, performs explicit disconnect ioctls (`NBD_CLEAR_SOCK` / `NBD_DISCONNECT`) on shutdown, and actively waits for block readiness before binding to USB Mass Storage.

### 5. Brazilian Portuguese (`pt-BR`) Localization & ABNT2 Keyboard
* **Full Brazilian Portuguese translation ([pt.json](./ui/src/locales/pt.json))**:
  * Complete `pt-BR` UI translation covering menus, settings, ATX power, virtual media, network, VPN, audio, video, terminal, diagnostics, and touch gesture instructions.
* **Brazilian ABNT2 keyboard layout (`pt_BR`) ([pt_BR.ts](./ui/src/keyboardLayouts/pt_BR.ts))**:
  * Full HID scancode mapping for ABNT2, including `ç`/`Ç` (`Semicolon`), slash/question mark (`IntlRo` and `AltGr+Q`/`AltGr+W`), backslash/pipe (`IntlBackslash`), dead-key accents (`´`, `` ` ``, `~`, `^`, `¨`), and `AltGr` combinations.

### 6. Mobile Experience & Touch Control (Android / iOS)
* **Uncropped full-screen video ([DesktopMobile.tsx](./ui/src/layout/core/desktop/DesktopMobile.tsx), [useTouchZoom.ts](./ui/src/layout/core/desktop/hooks/useTouchZoom.ts))**:
  * Uses `object-fit: contain` in both portrait and landscape orientations so 100% of the remote desktop is visible without cropping OS taskbars or menus.
  * Clamps pan/zoom boundaries to the actual video content area (excluding letterbox bars) and recalculates automatically via `ResizeObserver` on screen rotation or keyboard toggle.
* **Serialized virtual keyboard tap queue ([hidTapQueue.ts](./ui/src/utils/hidTapQueue.ts), [useKeyboardTaps.ts](./ui/src/hooks/useKeyboardTaps.ts), [VirtualKeyboard.tsx](./ui/src/components/VirtualKeyboard.tsx))**:
  * Replaces concurrent 100 ms release timers with a serialized queue of `press` (35 ms) + `release` (15 ms) pairs, preserving repeated letters during fast typing and guaranteeing key release on keyboard close, blur, or session change.
  * Compact 4-row mobile keyboard with full ABNT2 support (`ç`, `AltGr`, `\`, dead-key accents, and dynamic layout labels) and single-tap open from the bottom bar.
* **Precision touch mouse gestures with and without zoom ([useTouchGestures.ts](./ui/src/layout/core/desktop/hooks/useTouchGestures.ts), [useMouseEvents.ts](./ui/src/layout/core/desktop/hooks/useMouseEvents.ts))**:
  * **1 finger**: moves the cursor without unintended dragging (`buttons = 0`), even while zoomed in; a quick tap emits a left click on release.
  * **Long press (1 finger)**: emits only a right click (`BUTTON_RIGHT`) without firing a prior left click.
  * **Double-tap & drag**: holds the left mouse button on the second touch to select text or drag windows.
  * **2 fingers**: pinch-to-zoom and pan the image (or scroll the remote host when scroll mode is selected), immediately cancelling any pending single-finger tap.
  * **Relative mouse mode on touch**: accumulates subpixel deltas and splits large movements (`> 127 px`) into valid 8-bit signed HID reports without requiring Pointer Lock.

### 7. Credits & Dual GitHub Update Sources (`VersionContent.tsx`, `ota.go`)
* Displays project credits and clickable links to both repositories directly in **Settings → Version**.
* Allows selecting the OTA update source between **`github (jvcardoso)`** (this fork's releases with automatic fallback to Luckfox) and **`github (LuckfoxTECH)`** (official upstream releases), in addition to `custom` and `local`.

## Compilation (English)
* Compile frontend only:
  ```bash
  make frontend
  ```
* Compile backend only (development):
  ```bash
  make build_dev
  ```
* Full release compilation (frontend + ARMv7 backend):
  ```bash
  make build_release
  ```
* Upload the generated **`bin/kvm_app`** to Luckfox PicoKVM via local OTA package (`/ota/upload-local-pkg`) or via SSH/MTP replacing **`/userdata/picokvm/bin/kvm_app`**.

## Official Upstream Documentation
* [Luckfox PicoKVM Official Wiki](https://wiki.luckfox.com/Luckfox-PicoKVM/)
