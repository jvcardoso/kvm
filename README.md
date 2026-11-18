![luckfox](https://github.com/LuckfoxTECH/luckfox-pico/assets/144299491/cec5c4a5-22b9-4a9a-abb1-704b11651e88)
[中文](./README_CN.md)

# Luckfox PicoKVM

Luckfox PicoKVM é uma ferramenta leve de operação e manutenção IP KVM baseada no desenvolvimento secundário do [JetKVM](https://jetkvm.com/). Permite acesso remoto ao vídeo do dispositivo-alvo e emulação de entrada HID (teclado, mouse, touchscreen) e mídia virtual pela rede para administração sem contato de placas de desenvolvimento, PCs e servidores.

## Funcionalidades Principais
* **Suporte a cartão Micro SD**: configurações de boot ou expansão de armazenamento para imagens ISO/IMG
* **USB Multifunção (ConfigFS)**: emulação combinada de Teclado/Mouse HID, Mass Storage, placa de som USB (UAC1) ou dispositivo MTP com upload/download HTTP
* **Controle de porta serial**: terminal serial integrado para depuração do equipamento controlado
* **Controle de nível IO / ATX**: acionamento de alimentação e GPIOs de expansão
* **Tela sensível ao toque de 1,54"**: exibição de IP, estado de conexão e telemetria do sistema
* **Acesso remoto WebRTC e VPN**: gerenciamento baixa latência via WebRTC, proxy reverso FRP, Tailscale, ZeroTier, EasyTier e WireGuard

---

## Melhorias Implementadas sobre o Projeto Original

Esta versão inclui um conjunto abrangente de correções de segurança, estabilidade, concorrência, USB ConfigFS, mídia virtual, internacionalização (`pt-BR` / ABNT2) e usabilidade móvel.

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

---

## Compilação

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

## Documentação Original
* [Wiki Oficial Luckfox PicoKVM](https://wiki.luckfox.com/Luckfox-PicoKVM/)
