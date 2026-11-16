// Планета Meridian для настольных клиентов (Wails v3, HTML/CSS/JS).
//
// ПЕРЕНОС 1:1 С ANDROID: app/src/main/java/org/meridianvpn/app/Planet.kt,
// функция Planet(). Числа, цвета и порядок отрисовки те же; меняя что-то
// здесь, меняйте и там, иначе клиенты разъедутся. Отличие одно: вместо
// Compose Canvas — HTML <canvas>.
//
// Подключение:
//   <canvas id="planet"></canvas>
//   <script src="planet.js"></script>
//   const p = new MeridianPlanet(document.getElementById('planet'), 220);
//   p.setState('off' | 'busy' | 'connected');
//   p.setEnabled(true | false);   // только курсор и доступность, рисунок тот же
//
// Нажатие обрабатывает приложение (click на canvas): off → подключить,
// busy → остановить подключение, connected → отключить.

(function () {
  'use strict';

  // Цвета — как Brand в Planet.kt.
  const BRAND = {
    space: '#000000',       // фон и «тело» сферы — всегда (решение владельца 26.09)
    edge: '#6F98FF',        // контур и точки при туннеле, спутник всегда
    meridian: '#525D80',    // меридианы при туннеле
    edgeOff: '#3A4358',     // контур и точки без связи
    meridianOff: '#272D3A', // меридианы без связи
  };

  const REST_PHASE = 1.274;     // 73°: при этой фазе сфера выглядит как логотип
  const SIXTY_DEG = 1.0472;     // три плоскости меридианов через 60°
  const MERIDIAN_SPEED = 0.33;  // рад/с, оборот ~19 с
  const SAT_SPEED_UP = 0.78;    // рад/с, спутник при туннеле, оборот ~8 с
  const SAT_SPEED_BUSY = 2.62;  // рад/с, спутник при подключении, оборот ~2,4 с
  const FLASH_WINDOW = 0.30;    // окно вспышки у полюса, в единицах |cos| орбиты

  // Звезда на орбите — дизайн H (веб-панель роутера), одинаково во
  // всех клиентах. Размеры в долях r.
  const STAR_RX = 1.44, STAR_RY = 0.27; // полуоси орбиты
  const STAR_SPEED = 0.52;              // рад/с, оборот ~12 с
  const STAR_SPIN = 0.6;                // поворот искры = θ·0.6
  const STAR_S = 0.115;                 // конец луча
  const STAR_Q = 0.145;                 // опорные точки кривых, в долях s
  const STAR_GLOW = 0.167;              // радиус свечения
  // Ширина холста в r: орбита со свечением занимает 2·(1.44+0.167) ≈ 3.21r;
  // 3.3r = 305 px влезает и при полосе прокрутки (360 − 2·16 − 8 = 320).
  const WIDTH_R = 3.3;
  // Высота холста в r: подсветка контура доходит до 1.45r, на 2.9r она
  // не обрезается прямыми краями (при 0.42 от 220 обрезалась сверху и снизу).
  const HEIGHT_R = 2.95;

  class MeridianPlanet {
    constructor(canvas, sizePx) {
      this.canvas = canvas;
      this.size = sizePx || 220; // CSS-пиксели; на Android 220 dp
      this.state = 'off';
      this.enabled = true;
      // relay — решение владельца 25.09, уточнено 26.09: туннель поднят
      // через релей (белый режим) — контур, точки на полюсах, спутник и
      // меридианы рисуются чисто белым (#FFFFFF). Внутренность сферы
      // чёрная всегда, в любом режиме (как whiteNow в Planet.kt).
      this.relay = false;
      this.t = 0;          // секунды с начала движения, ноль в покое
      this.start = null;   // момент начала движения (performance.now)
      // Фаза спутника копится по кадрам, скорость плавно тянется к целевой:
      // t·speed при смене скорости перебрасывал спутник в другое место.
      this.satPhase = 0;
      this.satSpeed = SAT_SPEED_BUSY;
      this.lastFrame = 0;
      this.raf = 0;
      this._resize();
      window.addEventListener('resize', () => this._resize());
      canvas.addEventListener('mousemove', (ev) => {
        canvas.style.cursor = this.enabled && this.hit(ev) ? 'pointer' : 'default';
      });
      this.draw();
    }

    setEnabled(v) {
      this.enabled = !!v;
      if (!this.enabled) this.canvas.style.cursor = 'default';
      this._aria();
    }

    // setRelay — см. this.relay. Действует только пока state==='connected'
    // (draw() сама это учитывает) — звать можно в любой момент.
    setRelay(v) {
      v = !!v;
      if (v === this.relay) return;
      this.relay = v;
      this.draw();
    }

    setState(s) {
      if (s !== 'off' && s !== 'busy' && s !== 'connected') return;
      const wasMoving = this._moving();
      this.state = s;
      const moving = this._moving();
      // Как LaunchedEffect(moving) в Planet.kt: время сбрасывается ТОЛЬКО
      // при остановке. Переход busy → connected движение не прерывает.
      if (moving && !wasMoving) {
        this.start = performance.now();
        this.lastFrame = this.start;
        this._loop();
      } else if (!moving && wasMoving) {
        cancelAnimationFrame(this.raf);
        this.raf = 0;
        this.t = 0;
        this.start = null;
        this.satPhase = 0;
        this.satSpeed = SAT_SPEED_BUSY;
        this.draw();
      } else {
        this.draw();
      }
      this._aria();
    }

    _moving() { return this.state === 'busy' || this.state === 'connected'; }

    _aria() {
      const state = !this.enabled ? 'недоступно'
        : this.state === 'busy' ? 'подключаюсь'
        : this.state === 'connected' ? 'подключено' : 'отключено';
      this.canvas.setAttribute('role', 'button');
      this.canvas.setAttribute('aria-label', 'Планета Meridian, кнопка подключения: ' + state);
      this.canvas.setAttribute('aria-disabled', String(!this.enabled));
    }

    _width() { return Math.round(this.size * 0.42 * WIDTH_R); }
    _height() { return Math.round(this.size * 0.42 * HEIGHT_R); }

    // hit — нажатие попало в сферу (с запасом 1.1 r), а не в пустое место
    // по бокам холста, оставленное под орбиту.
    hit(ev) {
      const r = this.size * 0.42;
      const dx = ev.offsetX - this._width() / 2, dy = ev.offsetY - this._height() / 2;
      return dx * dx + dy * dy <= 1.21 * r * r;
    }

    _resize() {
      const dpr = window.devicePixelRatio || 1;
      this.canvas.style.width = this._width() + 'px';
      this.canvas.style.height = this._height() + 'px';
      this.canvas.width = Math.round(this._width() * dpr);
      this.canvas.height = Math.round(this._height() * dpr);
      this.ctx = this.canvas.getContext('2d');
      this.ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      this.draw();
    }

    // Кадры запрашиваются только пока планета движется. Неподвижная не
    // стоит ни одного кадра — как на Android.
    _loop() {
      const tick = (now) => {
        if (!this._moving()) return;
        this.t = (now - this.start) / 1000;
        const dt = Math.max(0, (now - this.lastFrame) / 1000);
        this.lastFrame = now;
        const target = this.state === 'connected' ? SAT_SPEED_UP : SAT_SPEED_BUSY;
        this.satSpeed += (target - this.satSpeed) * Math.min(1, dt * 3);
        this.satPhase += this.satSpeed * dt;
        this.draw();
        this.raf = requestAnimationFrame(tick);
      };
      cancelAnimationFrame(this.raf);
      this.raf = requestAnimationFrame(tick);
    }

    draw() {
      const g = this.ctx;
      if (!g) return;
      const W = this._width(), H = this._height();
      g.clearRect(0, 0, W, H);

      const cx = W / 2, cy = H / 2;
      const r = this.size * 0.42; // остаток по высоте — спутнику на контуре
      const connected = this.state === 'connected';
      const moving = this._moving();

      const white = connected && this.relay;
      const sphereColor = BRAND.space;
      const edgeColor = white ? '#FFFFFF' : connected ? BRAND.edge : BRAND.edgeOff;
      const meridianColor = white ? '#FFFFFF' : connected ? BRAND.meridian : BRAND.meridianOff;

      const edgeWidth = r * 0.045;
      const meridianWidth = r * 0.022;
      const dot = r * 0.075;
      const phase = REST_PHASE + this.t * MERIDIAN_SPEED;

      // Звезда — пока планета движется, как спутник (Planet.kt: moving).
      const theta = this.t * STAR_SPEED;
      const starNear = Math.sin(theta) >= 0;
      if (moving) {
        orbitArc(g, cx, cy, r, true);
        if (!starNear) star(g, cx, cy, r, theta);
      }

      // Мягкая подсветка контура (стекло, Planet.kt 0.1.209): свечение цветом
      // контура, 0 до 0.8r, 42% на контуре, 16% на 1.12r, 0 на 1.45r. Только
      // пока туннель работает. Внутренность потом закрывает тело сферы.
      if (connected) {
        const halo = r * 1.45;
        const hg = g.createRadialGradient(cx, cy, 0, cx, cy, halo);
        hg.addColorStop(0, withAlpha(edgeColor, 0));
        hg.addColorStop(0.80 / 1.45, withAlpha(edgeColor, 0));
        hg.addColorStop(1 / 1.45, withAlpha(edgeColor, 0.42));
        hg.addColorStop(1.12 / 1.45, withAlpha(edgeColor, 0.16));
        hg.addColorStop(1, withAlpha(edgeColor, 0));
        g.fillStyle = hg;
        g.beginPath();
        g.arc(cx, cy, halo, 0, Math.PI * 2);
        g.fill();
      }

      // Тело сферы.
      circle(g, cx, cy, r, sphereColor);

      // Меридианы: эллипсы высотой 2r и шириной 2r·|cos(долгота)|.
      g.strokeStyle = meridianColor;
      g.lineWidth = meridianWidth;
      for (let i = 0; i < 3; i++) {
        const rx = r * Math.abs(Math.cos(phase + i * SIXTY_DEG));
        if (rx < meridianWidth) continue; // встал ребром
        g.beginPath();
        g.ellipse(cx, cy, rx, r, 0, 0, Math.PI * 2);
        g.stroke();
      }

      // Контур.
      g.strokeStyle = edgeColor;
      g.lineWidth = edgeWidth;
      g.beginPath();
      g.arc(cx, cy, r, 0, Math.PI * 2);
      g.stroke();

      // Точки на полюсах.
      circle(g, cx, cy - r, dot, edgeColor);
      circle(g, cx, cy + r, dot, edgeColor);

      // Спутник: бежит по контуру, всегда поверх. Быстро — подключение,
      // спокойно — туннель поднят.
      if (moving) {
        const orbit = this.satPhase;
        const co = Math.cos(orbit), so = Math.sin(orbit);
        const sx = cx + r * co, sy = cy + r * so;

        // Вспышка при встрече с полюсом.
        const toPole = Math.abs(co);
        if (toPole < FLASH_WINDOW) {
          const k = 1 - toPole / FLASH_WINDOW;
          const gk = k * k;
          const glow = dot * (2.4 + 2.2 * gk);
          const ay = so > 0 ? cy + r : cy - r; // южный полюс при sin > 0
          const grad = g.createRadialGradient(cx, ay, 0, cx, ay, glow);
          grad.addColorStop(0, 'rgba(255,255,255,' + (0.95 * gk) + ')');
          grad.addColorStop(0.5, 'rgba(255,255,255,' + (0.30 * gk) + ')');
          grad.addColorStop(1, 'rgba(255,255,255,0)');
          g.fillStyle = grad;
          g.beginPath();
          g.arc(cx, ay, glow, 0, Math.PI * 2);
          g.fill();
          circle(g, cx, ay, dot * 0.9, 'rgba(255,255,255,' + gk + ')');
        }

        circle(g, sx, sy, dot * 0.85, edgeColor);
      }

      if (moving) {
        orbitArc(g, cx, cy, r, false);
        if (starNear) star(g, cx, cy, r, theta);
      }
    }
  }

  // orbitArc — верхняя (дальняя) дуга до сферы, нижняя (ближняя) после.
  function orbitArc(g, cx, cy, r, far) {
    g.save();
    g.strokeStyle = 'rgba(255,255,255,' + (far ? 0.10 : 0.16) + ')';
    g.lineWidth = r / 96;
    g.beginPath();
    g.ellipse(cx, cy, r * STAR_RX, r * STAR_RY, 0, far ? Math.PI : 0, far ? Math.PI * 2 : Math.PI);
    g.stroke();
    g.restore();
  }

  // star — белая четырёхлучевая искра со свечением; масштаб и прозрачность
  // по глубине (sin θ).
  function star(g, cx, cy, r, theta) {
    const sn = Math.sin(theta);
    const x = cx + r * STAR_RX * Math.cos(theta);
    const y = cy + r * STAR_RY * sn;
    const m = 0.72 + 0.28 * sn;
    const alpha = sn >= 0 ? 1 : 0.55 + 0.45 * (1 + sn);
    g.save();
    g.globalAlpha = alpha;
    g.translate(x, y);
    g.scale(m, m);

    const glow = r * STAR_GLOW;
    const grad = g.createRadialGradient(0, 0, 0, 0, 0, glow);
    grad.addColorStop(0, 'rgba(255,255,255,0.55)');
    grad.addColorStop(1, 'rgba(255,255,255,0)');
    g.fillStyle = grad;
    g.beginPath();
    g.arc(0, 0, glow, 0, Math.PI * 2);
    g.fill();

    g.rotate(theta * STAR_SPIN);
    const s = r * STAR_S, q = s * STAR_Q;
    g.fillStyle = '#FFFFFF';
    g.beginPath();
    g.moveTo(0, -s);
    g.bezierCurveTo(q, -q, q, -q, s, 0);
    g.bezierCurveTo(q, q, q, q, 0, s);
    g.bezierCurveTo(-q, q, -q, q, -s, 0);
    g.bezierCurveTo(-q, -q, -q, -q, 0, -s);
    g.closePath();
    g.fill();
    g.restore();
  }

  function withAlpha(hex, a) {
    const n = parseInt(hex.slice(1), 16);
    return 'rgba(' + (n >> 16) + ',' + ((n >> 8) & 255) + ',' + (n & 255) + ',' + a + ')';
  }

  function circle(g, x, y, rad, color) {
    g.fillStyle = color;
    g.beginPath();
    g.arc(x, y, rad, 0, Math.PI * 2);
    g.fill();
  }

  window.MeridianPlanet = MeridianPlanet;
})();
