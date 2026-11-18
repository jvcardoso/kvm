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

Esta versão do aplicativo (`AppVersion: 0.1.6`, testada sobre a imagem base `SystemVersion: 0.1.9` do dispositivo) inclui um conjunto de correções de segurança, estabilidade, concorrência, USB ConfigFS, mídia virtual, internacionalização (`pt-BR` / ABNT2), usabilidade móvel e seleção entre dois repositórios GitHub para atualização OTA do aplicativo.

> **Nota sobre o pacote `0.1.6`:** a release `0.1.6` e o pacote `picokvm_local_update_0.1.6.zip` contêm `version.txt`, `kvm_app` e `kvm_app.sha256` (`HasSystem=false`, sem `update_system.zip`). Eles atualizam **somente o aplicativo (`kvm_app` para `0.1.6`)**; o campo `SystemVersion: 0.1.9` no manifesto indica a versão do sistema base em execução no equipamento validado, não realizando atualização da imagem de sistema operacional.

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
* **Gravação atômica de configuração e preservação de arquivos corrompidos (`config.go`)**:
  * Escrita via arquivo temporário no mesmo diretório seguida de `Sync()`, `Close()`, `os.Rename()` atômico e `Sync()` do diretório pai para reduzir o risco de truncamento em reinicializações abruptas.
  * Caso um arquivo `/userdata/kvm_config.json` inválido ou corrompido seja encontrado na leitura, ele é preservado com sufixo `.corrupt.<timestamp>` para diagnóstico antes de aplicar a configuração padrão (não é mantido um backup rotativo `.bak`).
  * Migração segura de configurações legadas ou nulas sem sobrescrever credenciais nem falhar quando o cartão SD não está presente.
* **Sincronização de sessões WebRTC e timers (`web.go`, `webrtc.go`, `main.go`, `timesync.go`)**:
  * Gerenciamento concorrente de `currentSession` com `atomic.Pointer[Session]` (troca e comparação atômica `CompareAndSwap`/`Swap` ao desconectar ou substituir clientes).
  * Uso de `atomic.Bool` no estado de sincronização NTP (`timeSyncDone`) e limpeza de goroutines órfãs no coletor de estatísticas de rede.
* **Prevenção de deadlocks e corridas em JSON-RPC e OTA (`jsonrpc.go`, `ota.go`)**:
  * Liberação de locks de configuração antes de chamadas de subsistema (display, USB, áudio, rede) e serialização completa do fluxo de atualização OTA com verificação de hash SHA-256 antes da substituição do binário.
* **Deadlines e recuperação dos processos nativos (`native.go`, `native_display.go`, `native_audio.go`, `native_vpn.go`)**:
  * Aplicação de timeouts de leitura/escrita nos sockets Unix de vídeo, display, áudio e VPN, evitando bloqueio indefinido caso um processo auxiliar trave, com reconexão automática supervisionada.
* **Ativação atômica de ferramentas VPN (`tools.go`, `vpn_tool_exchange_*.go`)**:
  * Troca atômica de diretórios (`RENAME_EXCHANGE` no Linux) durante a instalação/atualização de binários de VPN (Tailscale, ZeroTier, EasyTier, WireGuard) com rollback automático em caso de falha.

### 3. USB Gadget e Recuperação HID (`internal/usbgadget`)
* **Mitigação do erro `failed to start kvm: -19` (`-ENODEV`) no controlador DWC3**:
  * O controlador USB do RK3506/RV1106 possui limite de endpoints de hardware. Quando `Audio` (`uac1.usb0`) ou `MTP` está habilitado, a função extra `hid.usb3` (pen/touchscreen) é automaticamente desabilitada em `HasPenTouch()`, permitindo que teclado (`hid.usb0`), mouse absoluto (`hid.usb1`), mouse relativo (`hid.usb2`) e áudio/mass-storage subam simultaneamente na combinação validada sem exaurir endpoints.
* **Verificação de unbind no rebind do ConfigFS**:
  * `configureUsbGadget()` executa `ensureGadgetUnbound()` dentro do ciclo `withHIDRebind`, fechando descritores `/dev/hidg*` abertos, desvinculando o `UDC` (`ffb00000.usb`) e interrompendo a reconfiguração caso o `UDC` permaneça vinculado após a tentativa de unbind (sem forçar o unbind do driver DWC3 da plataforma durante atualizações de configuração).

### 4. Mídia Virtual: NBD, WebRTC Disk e HTTP Range (`remote_mount.go`, `block_device.go`, `fuse.go`)
* **Montagem remota via WebRTC e HTTP**:
  * Implementação do hook `useWebRTCDisk` no frontend e validação de limites de offset/tamanho no canal `disk` do backend.
  * Suporte ao ciclo `mountWithHTTP` com verificação `HEAD`/Range, cache em memória com substituição de blocos (evitando crescimento ilimitado de RAM) e desmontagem limpa antes de trocar de mídia.
* **Dispositivo NBD resiliente**:
  * Abertura do `/dev/nbd0` com `O_RDWR`, desconexão explícita (`NBD_CLEAR_SOCK` / `NBD_DISCONNECT`) no encerramento e espera ativa pela prontidão do bloco antes de vinculá-lo ao gadget USB Mass Storage.

### 5. Internacionalização (`pt-BR`) e Teclado Brasileiro ABNT2
* **Dicionário completo em Português do Brasil ([pt.json](./ui/src/locales/pt.json))**:
  * Paridade completa de chaves com o dicionário inglês (`en.json`) cobrindo menus, configurações, energia/ATX, mídia virtual, rede, VPN, áudio, vídeo, terminal, diagnóstico, instruções de gestos e notificações de teclado/colagem (algumas notificações programáticas de erro em outros módulos do frontend ainda emitem strings literais em inglês).
* **Layout de teclado ABNT2 (`pt_BR`) ([pt_BR.ts](./ui/src/keyboardLayouts/pt_BR.ts))**:
  * Mapeamento para o padrão brasileiro ABNT2, incluindo `ç`/`Ç` (`Semicolon`), barra invertida/pipe (`IntlBackslash`), acentos/dead keys (`´`, `` ` ``, `~`, `^`, `¨`) e combinações com `AltGr` (como `AltGr+Q` para `/` e `AltGr+W` para `?`).
  * *Limitação conhecida (`IntlRo`)*: embora `IntlRo` esteja mapeado no frontend para `KeyRO` (usage HID `0x87` / 135), o descritor USB HID padrão para arranjos ANSI/ISO declara o intervalo de teclas `0..101` (`0x65`), enquanto o descritor estendido (`0..231`) só é ativado hoje no modo `JIS`. Por isso, sob o descritor padrão, `/` e `?` são atendidos via `AltGr+Q` e `AltGr+W`.

### 6. Experiência Mobile e Controle por Toque
* **Visualização em tela cheia sem cortes ([DesktopMobile.tsx](./ui/src/layout/core/desktop/DesktopMobile.tsx), [useTouchZoom.ts](./ui/src/layout/core/desktop/hooks/useTouchZoom.ts))**:
  * Vídeo com `object-fit: contain` em retrato e paisagem, exibindo 100% da área do monitor remoto sem cortar barras do sistema operacional.
  * Limite de pan/zoom restrito à área real da imagem (desconsiderando as faixas de *letterbox*) e recalculado automaticamente via `ResizeObserver` ao girar a tela ou abrir o teclado.
* **Teclado virtual móvel com fila serializada ([hidTapQueue.ts](./ui/src/utils/hidTapQueue.ts), [useKeyboardTaps.ts](./ui/src/hooks/useKeyboardTaps.ts), [VirtualKeyboard.tsx](./ui/src/components/VirtualKeyboard.tsx))**:
  * Substituição dos timers concorrentes de 100 ms por uma fila serializada de pares `press` + `release`, mantendo cada tecla pressionada por **35 ms** com intervalo de **15 ms** entre toques consecutivos. Isso preserva letras repetidas em digitação rápida e tenta enviar a liberação no `finally` pelo mesmo emissor capturado no início do toque (caso o canal de transporte continue disponível).
  * Teclado móvel compacto de 4 linhas com suporte ao layout ABNT2 (`ç`, `AltGr`, `\`, acentos e símbolos brasileiros). A abertura direta com 1 toque fica na **barra superior móvel e nas ferramentas flutuantes de borda** ([TopBarMobile.tsx](./ui/src/layout/core/bar_top/TopBarMobile.tsx), [bar_autohide](./ui/src/layout/core/bar_autohide/index.tsx)), enquanto o botão de teclado da barra inferior abre o painel de configurações de teclado ([BottomBarMobile.tsx](./ui/src/layout/core/bar_bottom/BottomBarMobile.tsx)).
* **Gestos de mouse precisos com e sem zoom ([useTouchGestures.ts](./ui/src/layout/core/desktop/hooks/useTouchGestures.ts), [useMouseEvents.ts](./ui/src/layout/core/desktop/hooks/useMouseEvents.ts))**:
  * **1 dedo**: move o cursor sem arrastar involuntariamente (`buttons = 0`), inclusive quando a imagem está ampliada; um toque rápido gera clique esquerdo ao soltar.
  * **Toque longo (1 dedo)**: envia apenas clique direito (`BUTTON_RIGHT`), sem disparar clique esquerdo antes.
  * **Duplo toque + arraste**: mantém o botão esquerdo pressionado no segundo toque para selecionar texto ou mover janelas.
  * **2 dedos**: pinça para zoom e pan da imagem (ou rolagem remota quando o modo de rolagem está ativo), cancelando imediatamente qualquer clique pendente do primeiro dedo.
  * **Modo relativo em telas touch**: acumula frações subpixel e divide movimentos grandes (`> 127 px`) em múltiplos relatórios HID de 8 bits sem depender de Pointer Lock.
* **Escopo da validação móvel**:
  * Os fluxos de toque, zoom e teclado virtual foram validados de ponta a ponta usando Chrome no macOS com emulação Android/touch conectado ao stream de vídeo e HID reais do Luckfox (`192.168.11.8`). Não foi realizada validação física em Safari/iOS nesta rodada, e a digitação utiliza o teclado virtual do próprio KVM (não há integração com o teclado nativo/IME do Android).

### 7. Créditos e Dupla Fonte de Atualização GitHub (`VersionContent.tsx`, `ota.go`)
* Exibição de créditos e links para os dois repositórios diretamente em **Settings → Version**.
* Seleção de fonte de atualização OTA entre **`github (jvcardoso)`** (consulta primeiro o fork `jvcardoso/kvm` com fallback para a Luckfox) e **`github (LuckfoxTECH)`** (consulta diretamente os repositórios originais da Luckfox sem passar pelo fork), além de `custom` e `local`.
* *Nota sobre atualização de sistema (`kvm_system`)*: a verificação de atualização do aplicativo (`kvm_app`) está validada nas duas fontes GitHub. Já a instalação remota de uma nova imagem de sistema (`kvm_system`) exige que o repositório selecionado publique um arquivo de release com hash SHA-256 ou assinatura criptográfica válida, pois `verifyFile` bloqueia pacotes sem verificação de integridade.

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

This application build (`AppVersion: 0.1.6`, tested on top of the device's base `SystemVersion: 0.1.9` image) introduces fixes and enhancements across security, backend concurrency, USB ConfigFS, virtual media, internationalization (`pt-BR` / ABNT2), mobile touch UX, and dual GitHub OTA update sources for the application.

> **Note on the `0.1.6` package:** release `0.1.6` and `picokvm_local_update_0.1.6.zip` contain `version.txt`, `kvm_app`, and `kvm_app.sha256` (`HasSystem=false`, without `update_system.zip`). They update **only the application (`kvm_app` to `0.1.6`)**; the `SystemVersion: 0.1.9` entry in `version.txt` identifies the base system version running on the validated device and does not perform an OS image update.

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
* **Atomic configuration writes & corrupted file preservation (`config.go`)**:
  * Writes configuration via a same-directory temporary file followed by `Sync()`, `Close()`, atomic `os.Rename()`, and parent directory `Sync()` to reduce truncation risk on unexpected restarts.
  * If an invalid or unparseable `/userdata/kvm_config.json` is encountered on load, it is preserved with a `.corrupt.<timestamp>` suffix for diagnosis before applying defaults (a rotating `.bak` backup is not created).
  * Safe migration of legacy or null configuration files without overwriting credentials or failing when no SD card is inserted.
* **WebRTC session & timer synchronization (`web.go`, `webrtc.go`, `main.go`, `timesync.go`)**:
  * Managed concurrent `currentSession` access via `atomic.Pointer[Session]` (using atomic `CompareAndSwap`/`Swap` on client disconnect or replacement).
  * Used `atomic.Bool` for NTP synchronization state (`timeSyncDone`) and cleaned up orphaned goroutines in the network statistics collector.
* **Deadlock & race condition prevention in JSON-RPC and OTA (`jsonrpc.go`, `ota.go`)**:
  * Released configuration locks prior to subsystem calls (display, USB, audio, network) and serialized the OTA update flow with SHA-256 verification before binary replacement.
* **Native process socket deadlines & recovery (`native.go`, `native_display.go`, `native_audio.go`, `native_vpn.go`)**:
  * Added read/write deadlines on Unix domain sockets for video, display, audio, and VPN helper processes to prevent indefinite stalls, paired with supervised auto-reconnection.
* **Atomic VPN tool activation (`tools.go`, `vpn_tool_exchange_*.go`)**:
  * Atomic directory exchange (`RENAME_EXCHANGE` on Linux) when installing or updating VPN binaries (Tailscale, ZeroTier, EasyTier, WireGuard) with automatic rollback on failure.

### 3. USB Gadget & HID Recovery (`internal/usbgadget`)
* **Mitigated DWC3 `failed to start kvm: -19` (`-ENODEV`) endpoint exhaustion**:
  * The RK3506/RV1106 USB controller has a hardware endpoint limit. When `Audio` (`uac1.usb0`) or `MTP` is enabled, the extra `hid.usb3` (pen/touchscreen) function is automatically disabled in `HasPenTouch()`, allowing keyboard (`hid.usb0`), absolute mouse (`hid.usb1`), relative mouse (`hid.usb2`), and audio/mass-storage to bind simultaneously in the validated configuration without exhausting endpoints.
* **Unbind verification during ConfigFS rebind**:
  * `configureUsbGadget()` executes `ensureGadgetUnbound()` inside `withHIDRebind`, closing open `/dev/hidg*` descriptors, unbinding the `UDC` (`ffb00000.usb`), and aborting reconfiguration if the `UDC` remains bound after the unbind attempt (without forcibly unbinding the platform DWC3 driver during routine config updates).

### 4. Virtual Media: NBD, WebRTC Disk & HTTP Range (`remote_mount.go`, `block_device.go`, `fuse.go`)
* **WebRTC & HTTP remote media mounting**:
  * Implemented the `useWebRTCDisk` frontend hook and backend offset/length bounds checking on the `disk` DataChannel.
  * Complete `mountWithHTTP` lifecycle with `HEAD`/Range validation, bounded in-memory block cache (preventing unbounded RAM growth), and clean unmounting before switching media.
* **Resilient NBD block device handling**:
  * Opens `/dev/nbd0` with `O_RDWR`, performs explicit disconnect ioctls (`NBD_CLEAR_SOCK` / `NBD_DISCONNECT`) on shutdown, and actively waits for block readiness before binding to USB Mass Storage.

### 5. Brazilian Portuguese (`pt-BR`) Localization & ABNT2 Keyboard
* **Brazilian Portuguese locale dictionary ([pt.json](./ui/src/locales/pt.json))**:
  * Full key parity with the English dictionary (`en.json`) covering menus, settings, ATX power, virtual media, network, VPN, audio, video, terminal, diagnostics, touch gesture instructions, and keyboard/paste notifications (some programmatic error notifications in other frontend modules still emit literal English strings).
* **Brazilian ABNT2 keyboard layout (`pt_BR`) ([pt_BR.ts](./ui/src/keyboardLayouts/pt_BR.ts))**:
  * ABNT2 mapping including `ç`/`Ç` (`Semicolon`), backslash/pipe (`IntlBackslash`), dead-key accents (`´`, `` ` ``, `~`, `^`, `¨`), and `AltGr` combinations (such as `AltGr+Q` for `/` and `AltGr+W` for `?`).
  * *Known limitation (`IntlRo`)*: although `IntlRo` is mapped in the frontend to `KeyRO` (HID usage `0x87` / 135), the default USB HID keyboard descriptor for ANSI/ISO arrangements declares usage range `0..101` (`0x65`), while the extended `0..231` descriptor is currently enabled only for `JIS`. Under the default descriptor, `/` and `?` are therefore typed via `AltGr+Q` and `AltGr+W`.

### 6. Mobile Experience & Touch Control
* **Uncropped full-screen video ([DesktopMobile.tsx](./ui/src/layout/core/desktop/DesktopMobile.tsx), [useTouchZoom.ts](./ui/src/layout/core/desktop/hooks/useTouchZoom.ts))**:
  * Uses `object-fit: contain` in both portrait and landscape orientations so 100% of the remote desktop is visible without cropping OS taskbars or menus.
  * Clamps pan/zoom boundaries to the actual video content area (excluding letterbox bars) and recalculates automatically via `ResizeObserver` on screen rotation or keyboard toggle.
* **Serialized virtual keyboard tap queue ([hidTapQueue.ts](./ui/src/utils/hidTapQueue.ts), [useKeyboardTaps.ts](./ui/src/hooks/useKeyboardTaps.ts), [VirtualKeyboard.tsx](./ui/src/components/VirtualKeyboard.tsx))**:
  * Replaces concurrent 100 ms release timers with a serialized queue of `press` + `release` pairs, holding each key press for **35 ms** with a **15 ms** gap between consecutive taps. This preserves repeated letters during fast typing and attempts key release in `finally` using the sender captured at tap start (provided the transport channel remains available).
  * Compact 4-row mobile keyboard with ABNT2 support (`ç`, `AltGr`, `\`, dead-key accents, and dynamic layout labels). Direct 1-tap toggle is located in the **top mobile bar and floating edge toolbar** ([TopBarMobile.tsx](./ui/src/layout/core/bar_top/TopBarMobile.tsx), [bar_autohide](./ui/src/layout/core/bar_autohide/index.tsx)), whereas the bottom bar keyboard button opens the Keyboard settings drawer ([BottomBarMobile.tsx](./ui/src/layout/core/bar_bottom/BottomBarMobile.tsx)).
* **Precision touch mouse gestures with and without zoom ([useTouchGestures.ts](./ui/src/layout/core/desktop/hooks/useTouchGestures.ts), [useMouseEvents.ts](./ui/src/layout/core/desktop/hooks/useMouseEvents.ts))**:
  * **1 finger**: moves the cursor without unintended dragging (`buttons = 0`), even while zoomed in; a quick tap emits a left click on release.
  * **Long press (1 finger)**: emits only a right click (`BUTTON_RIGHT`) without firing a prior left click.
  * **Double-tap & drag**: holds the left mouse button on the second touch to select text or drag windows.
  * **2 fingers**: pinch-to-zoom and pan the image (or scroll the remote host when scroll mode is selected), immediately cancelling any pending single-finger tap.
  * **Relative mouse mode on touch**: accumulates subpixel deltas and splits large movements (`> 127 px`) into valid 8-bit signed HID reports without requiring Pointer Lock.
* **Mobile validation scope**:
  * Touch mouse gestures, zoom clamping, and the virtual keyboard tap queue were validated end-to-end using Chrome on macOS with Android/touch emulation connected to the real Luckfox video and HID stream (`192.168.11.8`). Physical Safari/iOS testing was not performed in this cycle, and text entry uses the KVM's on-screen virtual keyboard (no native Android IME/Gboard integration).

### 7. Credits & Dual GitHub Update Sources (`VersionContent.tsx`, `ota.go`)
* Displays project credits and clickable links to both repositories directly in **Settings → Version**.
* Allows selecting the OTA update source between **`github (jvcardoso)`** (queries this fork first with automatic fallback to Luckfox) and **`github (LuckfoxTECH)`** (queries the official upstream repositories directly without checking the fork), in addition to `custom` and `local`.
* *Note on system updates (`kvm_system`)*: application update checks (`kvm_app`) are validated across both GitHub sources. Remote system image updates (`kvm_system`), however, require the selected repository to publish a release archive with a valid SHA-256 digest or cryptographic signature, as `verifyFile` rejects archives without integrity metadata.

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
