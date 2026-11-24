# Patylook

Sistema web de gestão de pedidos no estilo e-commerce, escrito em Go com MySQL e templates HTML.
Cobre cadastro de produtos (com preços e imagens), clientes, formas de pagamento e pedidos, com
usuários que gerenciam tudo por login. **Não há** pagamento online, integração com meios de
pagamento nem logística (entrega, rastreio): a forma de pagamento apenas classifica o pedido e
define um acréscimo percentual.

## Sumário

- [Funcionalidades](#funcionalidades)
- [Requisitos](#requisitos)
- [Banco de dados (já existente)](#banco-de-dados-já-existente)
- [Executando localmente](#executando-localmente)
- [Configuração](#configuração)
- [Imagens dos produtos](#imagens-dos-produtos)
- [Primeiro acesso e usuários](#primeiro-acesso-e-usuários)
- [Arquitetura](#arquitetura)
- [Testes e comandos](#testes-e-comandos)
- [Publicação em produção (HostGator)](#publicação-em-produção-hostgator)
- [Segurança](#segurança)
- [Limitações conhecidas](#limitações-conhecidas)

## Funcionalidades

| Área | O que faz |
|---|---|
| Autenticação | Login por usuário e senha, sessão no servidor com cookie, logout, troca da própria senha |
| Painel | Pedidos abertos e concluídos do mês, total concluído, produtos com estoque baixo, pedidos recentes |
| Produtos | Cadastro, edição, ativar/desativar, busca por nome ou SKU, categorias |
| Preços | Histórico de vigências por produto: custo, embalagem, logística, outros custos e markup geram o preço final |
| Imagens | Imagem principal obrigatória + carrossel por produto (veja [Imagens dos produtos](#imagens-dos-produtos)) |
| Mostruário | Banner de cada produto ativo; ao clicar, a imagem é destacada e as demais ficam num carrossel |
| Clientes | Cadastro com CPF validado, ativar/desativar, busca |
| Formas de pagamento | Cadastro com acréscimo percentual |
| Pedidos | Criação com vários itens, desconto por item e no pedido, acréscimo da forma de pagamento, conclusão e cancelamento |
| Usuários | Somente administradores: cadastro, edição, redefinição de senha, ativar/desativar |

Regras de negócio que valem saber:

- **Preço e custo são gravados no item do pedido** no momento da venda. Mudar o preço depois não
  altera pedidos antigos. A vigência usada é a mais recente que cobre a data do pedido.
- **Estoque** é reservado ao criar o pedido e devolvido se ele for cancelado. Um produto sem preço
  vigente, inativo ou sem estoque não pode ser vendido.
- **Status do pedido:** `open` → `completed` ou `canceled`; `completed` → `canceled`. Cancelado é final.
- **Valores monetários** são tratados em centavos inteiros, nunca em ponto flutuante.
- **Total do pedido** = subtotal (itens menos descontos de item) − desconto do pedido + acréscimo da
  forma de pagamento, que incide sobre o valor já com desconto.

## Requisitos

- Go **1.23** ou superior
- MySQL 5.7+ ou MariaDB (o banco `patylook` já deve existir, veja abaixo)
- Nenhuma dependência de Node/npm: CSS e JavaScript são arquivos simples embutidos no binário

## Banco de dados (já existente)

> **O banco de dados já existe e contém dados.** A aplicação **nunca** cria nem altera tabelas
> sozinha, e **não se deve executar o `schema.sql` inteiro**: ele contém `DROP TABLE` para todas as
> tabelas e apagaria os dados. O arquivo serve como documentação da estrutura.

O app só lê e grava dados. Na inicialização ele faz uma verificação somente leitura e se recusa a
subir, com mensagem explicando o que falta, se:

1. a tabela `user` não existir no banco configurado (`DB_NAME`);
2. a coluna `user.password` for estreita demais para um hash bcrypt;
3. a tabela `product_images` não existir.

### Ajustes necessários no banco

**a) Coluna de senha.** O schema original declara `user.password` como `VARCHAR(32)`, mas um hash
bcrypt tem 60 caracteres. Execute **uma vez**:

```sql
ALTER TABLE `user` MODIFY COLUMN `password` VARCHAR(255) NOT NULL;
```

(o mesmo comando está em `migrations/001_widen_user_password.sql`; só alarga a coluna, não altera
nem remove dados).

**b) Tabela `product_images`.** Execute apenas o bloco dela, que está no final do `schema.sql`
(a seção `Table patylook.product_images`). Os caminhos das imagens de cada produto ficam nela.

### Senhas de usuários que já existiam

Senhas gravadas no formato antigo (até 32 caracteres) **não funcionam** no login. Gere um hash e
grave no usuário:

```bash
go run ./cmd/patylook hash-password
```

```sql
UPDATE `user` SET `password` = '<hash gerado>' WHERE `username` = '<login>';
```

O comando só gera o hash; não acessa o banco. A senha é lida do terminal (sem eco) e precisa ter de
8 a 72 caracteres.

## Executando localmente

1. Tenha o MySQL rodando com o banco `patylook` e os ajustes acima aplicados.
2. Crie o arquivo de configuração a partir do exemplo e confira as credenciais do banco:

   ```bash
   cp .env.example .env
   ```

   O exemplo já vem com as credenciais locais (`localhost`, `root`, sem senha, banco `patylook`).

3. Suba o servidor:

   ```bash
   go run ./cmd/patylook
   ```

4. Abra **http://localhost:8080**. Sem login, você é redirecionado para `/login`.

Se a porta 8080 estiver ocupada, mude `APP_ADDR` (por exemplo `:3000`). Para parar, use `Ctrl+C`:
o servidor encerra de forma ordenada.

Para verificar rapidamente se o processo está de pé: `GET /healthz` responde `ok`.

## Configuração

Tudo vem de variáveis de ambiente. Opcionalmente o app lê um arquivo `.env` na pasta atual (ou o
indicado em `-env caminho`); **variáveis de ambiente reais sempre prevalecem** sobre o arquivo.
Comentários no `.env` devem ficar em linhas próprias, não depois do valor.

| Variável | Padrão | Descrição |
|---|---|---|
| `APP_ENV` | `development` | `development` ou `production` |
| `APP_MODE` | `http` | `http` (servidor próprio) ou `fcgi` (atrás de servidor web com FastCGI) |
| `APP_ADDR` | `:8080` | Endereço de escuta no modo `http`. `PORT` também é aceita |
| `APP_TIMEZONE` | `America/Sao_Paulo` | Fuso para datas e horários; também é usado como fuso da sessão MySQL |
| `SESSION_TTL` | `8h` | Tempo de inatividade até a sessão expirar (`30m`, `8h`...) |
| `COOKIE_SECURE` | `true` em produção | Marca o cookie como `Secure` (só HTTPS). Mantenha `false` em `http://localhost` |
| `DB_HOST` | `127.0.0.1` | Servidor MySQL |
| `DB_PORT` | `3306` | Porta |
| `DB_USER` | (obrigatória) | Usuário |
| `DB_PASSWORD` | vazio | Senha |
| `DB_NAME` | `patylook` | Nome do banco |
| `DB_TLS` | vazio | `true`, `skip-verify` ou `preferred`, se o servidor exigir TLS |
| `ADMIN_CATEGORY` | `admin` | Nome da categoria de usuário (`user_category`) com permissão administrativa |
| `IMAGES_DIR` | `images/products` | Pasta com uma subpasta por produto |
| `BOOTSTRAP_ADMIN_USERNAME` / `_NAME` / `_PASSWORD` | vazio | Criação do primeiro administrador (veja abaixo) |

> ⚠️ **As credenciais do `.env` são apenas locais** (`localhost`, `root`, sem senha). **Ao subir para
> produção, altere `DB_HOST`, `DB_USER`, `DB_PASSWORD` e `DB_NAME`** e defina `APP_ENV=production`.
> O app registra um aviso no log se iniciar em produção com `root` sem senha. O `.env` está no
> `.gitignore` e não deve ser versionado.

## Imagens dos produtos

Cada produto tem uma pasta própria, nomeada pelo SKU:

```
images/products/
└── CAM-1/
    ├── principal.jpg      # obrigatória: banner do produto no mostruário
    ├── imagem-01.jpg      # opcionais: compõem o carrossel
    └── imagem-02.jpg
```

Na tabela `product_images` cada arquivo é registrado com seu caminho relativo
(`images/products/CAM-1/principal.jpg`), nome, ordem no carrossel e quem enviou.

**Onde enviar:** no cadastro de **novo produto** (imagem principal obrigatória e até 8 adicionais) e,
para produtos existentes, na seção **Imagens** da tela de edição, onde também é possível substituir a
principal e remover imagens do carrossel.

**O que o sistema faz com o arquivo enviado:**

- aceita **JPEG ou PNG**, até **6 MB** cada, com no mínimo 200 px no menor lado;
- valida pelo **conteúdo**, não pelo nome nem pelo tipo informado pelo navegador;
- corrige a orientação de fotos de celular (EXIF), achata transparência sobre fundo branco e reduz
  para no máximo 1600 px no maior lado;
- grava **sempre como JPEG**, descartando metadados (localização, dados da câmera);
- se a gravação em disco ou no banco falhar, desfaz as duas coisas: não sobram produto sem imagem
  nem arquivos órfãos.

**Regras:**

- A imagem principal **não pode ser removida**, apenas substituída.
- O **SKU não pode ser alterado enquanto o produto tiver imagens**, pois ele nomeia a pasta.
- No nome da pasta, caracteres inseguros do SKU (`/`, espaço, acentos...) viram `-`. Dois SKUs que
  gerem a mesma pasta não podem coexistir (o sistema avisa).
- As imagens são servidas em `/images/products/<pasta>/<arquivo>` **somente para usuários logados**.

> Em produção, a pasta de imagens precisa de **permissão de escrita** para o usuário que roda o app e
> deve entrar no **backup** junto com o banco. Ela não faz parte do binário.

## Primeiro acesso e usuários

Há dois caminhos para ter um usuário que consiga entrar:

- **Tabela `user` vazia:** defina no `.env` (ou no ambiente) `BOOTSTRAP_ADMIN_USERNAME`,
  `BOOTSTRAP_ADMIN_NAME` e `BOOTSTRAP_ADMIN_PASSWORD` (mínimo 8 caracteres). Na primeira inicialização
  o administrador é criado, junto com a categoria `ADMIN_CATEGORY` se ela não existir. **Remova essas
  variáveis depois.**
- **Já existem usuários:** use o `hash-password` descrito em
  [Senhas de usuários que já existiam](#senhas-de-usuários-que-já-existiam).

Quem pertence à categoria definida em `ADMIN_CATEGORY` vê o menu **Usuários** e pode gerenciar
usuários; os demais usuários operam produtos, clientes e pedidos, mas não gerenciam usuários.
Desativar um usuário ou redefinir/alterar a senha encerra as sessões abertas dele.

## Arquitetura

Arquitetura hexagonal (portas e adaptadores): o núcleo de regras não conhece banco, HTTP nem disco;
os adaptadores dependem do núcleo, nunca o contrário.

```
cmd/patylook/            ponto de entrada: monta as dependências, sobe o servidor, comando hash-password
internal/
├── core/                NÚCLEO (sem dependência de infraestrutura)
│   ├── domain/          entidades e regras: dinheiro, CPF, preço, pedido, imagem, erros
│   ├── port/            interfaces: inbound (casos de uso) e outbound (repositórios, storage, hash)
│   └── service/         casos de uso: auth, usuários, catálogo/imagens, clientes, pedidos, painel
├── adapter/
│   ├── web/             adaptador de entrada: rotas, middlewares, handlers, renderização
│   ├── mysql/           adaptador de saída: repositórios (database/sql), transações
│   ├── filestore/       adaptador de saída: imagens no disco
│   ├── memory/          adaptador de saída: sessões em memória
│   └── security/        adaptador de saída: hash de senha (bcrypt)
└── config/              leitura de variáveis de ambiente e .env
web/
├── templates/           layout, partials e páginas HTML (embutidos no binário)
└── static/              CSS e JavaScript (embutidos no binário)
migrations/              scripts SQL opcionais e aditivos para o banco existente
images/products/         imagens dos produtos (fora do binário)
```

Pontos de design:

- **Camadas separadas:** persistência (`adapter/mysql`), controladores (`adapter/web/handlers_*.go`) e
  views (`web/templates`) não se misturam; a regra de negócio fica em `core`.
- **Transações** via porta `Transactor`: os repositórios participam da transação carregada no
  `context`. Criar pedido (inclusive baixa de estoque) e cancelar (devolução) são atômicos.
- **Estoque seguro sob concorrência:** a baixa é um `UPDATE ... WHERE stock >= ?`, e itens são
  processados em ordem fixa de produto para evitar deadlocks.
- **Dependências mínimas:** biblioteca padrão (`net/http` com roteamento por método e `html/template`),
  `go-sql-driver/mysql`, `x/crypto` (bcrypt), `x/image` (redimensionamento) e `x/term` (senha no terminal).
- **Binário único:** templates e arquivos estáticos são embutidos (`go:embed`). Só o `.env` e a pasta de
  imagens ficam fora.

## Testes e comandos

```bash
go test ./...        # todos os testes
go vet ./...         # análise estática
go run ./cmd/patylook                 # servidor local
go run ./cmd/patylook hash-password   # gera hash bcrypt de uma senha
```

O `Makefile` oferece atalhos (`make test`, `make run`, `make build`, `make build-linux`, `make fmt`)
para quem tem `make` instalado.

Os testes cobrem regras de domínio (dinheiro, CPF, cálculo de pedido), serviços (pedidos, estoque,
autenticação, imagens, rollback) com repositórios falsos, o processamento de imagens, o
armazenamento em disco (incluindo tentativas de _path traversal_) e a camada web (login, CSRF,
permissões, renderização de todas as páginas, upload). **Os testes não usam um MySQL real**: as
consultas SQL são verificadas na prática ao rodar o sistema contra o banco.

## Publicação em produção (HostGator)

### Antes de publicar

1. **Altere as credenciais do banco** (`DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`) para as do
   MySQL do servidor. Em hospedagem compartilhada o nome do banco e do usuário costumam ter o prefixo
   da conta (ex.: `conta_patylook`).
2. Defina `APP_ENV=production` (ativa cookie `Secure` e logs em JSON).
3. Aplique no banco de produção os [ajustes necessários](#ajustes-necessários-no-banco) (coluna de
   senha e tabela `product_images`), **sem** rodar o `schema.sql` completo.
4. Garanta um usuário administrador ([Primeiro acesso](#primeiro-acesso-e-usuários)).
5. Publique o sistema atrás de **HTTPS**: o cookie de sessão só é enviado por conexão segura.

### Gerando o binário

```bash
# Linux 64 bits, estático (templates e assets embutidos)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o dist/patylook ./cmd/patylook
```

(ou `make build-linux`). No PowerShell, defina antes as variáveis:
`$env:CGO_ENABLED=0; $env:GOOS="linux"; $env:GOARCH="amd64"`.

Envie `dist/patylook` para o servidor e crie ao lado dele a pasta `images/products/` com permissão
de escrita.

### Onde isso roda de verdade

> **A HostGator não oferece suporte oficial a aplicações Go em hospedagem compartilhada.** O caminho
> mais seguro é um **VPS/servidor dedicado** (ou outra hospedagem que permita processos
> persistentes), onde o app roda como serviço atrás de um proxy HTTPS.

**VPS (recomendado).** Exemplo de serviço `systemd` (`/etc/systemd/system/patylook.service`):

```ini
[Unit]
Description=Patylook
After=network.target mysql.service

[Service]
WorkingDirectory=/opt/patylook
ExecStart=/opt/patylook/patylook
Restart=on-failure
User=patylook
Environment=APP_ENV=production
Environment=APP_ADDR=127.0.0.1:8080
Environment=DB_HOST=127.0.0.1
Environment=DB_USER=...
Environment=DB_PASSWORD=...
Environment=DB_NAME=...
# Restrinja o acesso ao arquivo de unidade, pois ele contém a senha do banco.

[Install]
WantedBy=multi-user.target
```

Coloque Nginx ou Apache na frente como proxy reverso com certificado HTTPS apontando para
`127.0.0.1:8080`. A pasta `/opt/patylook/images/products` deve pertencer ao usuário `patylook`.

**Hospedagem compartilhada (experimental, não testado).** Existe o modo `APP_MODE=fcgi`, em que o app
fala FastCGI na entrada padrão para ser chamado pelo servidor web, usando `net/http/fcgi`. Ele **não
foi validado** em ambiente HostGator; a viabilidade depende de o plano permitir executar binário
próprio e configurar o roteamento (`.htaccess`). Confirme com o suporte da hospedagem antes de
depender disso.

### Observações de operação

- **Sessões ficam em memória:** reiniciar o processo desconecta todos os usuários.
- **Fuso horário:** `APP_TIMEZONE` também define o fuso da sessão MySQL; `TIMESTAMP`s existentes são
  exibidos corretamente nesse fuso.
- **Logs** vão para a saída padrão (texto em desenvolvimento, JSON em produção).
- **Backup:** banco **e** `images/products/` precisam ser copiados juntos.

## Segurança

- Senhas com **bcrypt** (custo 12); política de 8 a 72 caracteres.
- Sessão no servidor: o cookie guarda um token aleatório (`HttpOnly`, `SameSite=Lax`, `Secure` em
  produção) e o servidor guarda apenas o **hash** dele. Expiração por inatividade.
- **CSRF:** token por sessão exigido em todo envio de formulário autenticado, mais bloqueio de
  requisições de outra origem (`Sec-Fetch-Site`/`Origin`).
- **Força bruta:** após 5 falhas de login em 15 minutos (por IP e usuário), novas tentativas são
  bloqueadas. O tempo de resposta não revela se o usuário existe.
- Cabeçalhos de segurança: CSP restritiva (sem scripts inline), `X-Frame-Options`,
  `X-Content-Type-Options`, `Referrer-Policy` e HSTS quando o cookie é `Secure` (produção).
- SQL sempre parametrizado; saída HTML escapada pelo `html/template`.
- Uploads: tamanho limitado antes da leitura, validação por conteúdo, limite de dimensões contra
  _decompression bombs_, reencodificação (remove conteúdo embutido) e nomes de arquivo gerados pelo
  servidor. A leitura de imagens aceita apenas nomes que a aplicação gerou, impedindo _path traversal_.
- Desativar usuário, redefinir ou trocar a senha revoga as sessões dele.

## Limitações conhecidas

- Sessões em memória (perdem-se ao reiniciar). Com mais de uma instância do app seria preciso mover
  as sessões para o banco ou outro armazenamento compartilhado.
- Não há gerenciamento de categorias de usuário pela interface (use SQL).
- Pedidos não podem ser editados depois de criados; só concluídos ou cancelados.
- O app não faz migrações automáticas: mudanças de estrutura do banco são aplicadas manualmente.
- Cliente identificado apenas por CPF (11 dígitos), conforme a coluna `customer.document`.
