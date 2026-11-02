import { Stage } from '../hero/kit/stage'
import { Phone } from '../hero/kit/phone'
import './store.css'

// The App Store screenshots, 1320 × 2868 (Apple's 6.9-inch size): the runyard lockup, a caption with one word
// in its state colour, and the real app on the built-in demo, captured on an iPhone (screens/), inside the
// kit's iPhone. render.sh films them.
const shots = import.meta.glob<string>('./screens/*.png', { eager: true, import: 'default' })

// [before, word, after, colour of the word]: teal is alive, amber is crashed or silent, as in the app
const TEAL = '#2dd4bf'
const AMBER = '#fbbf24'
export const SHOTS = [
  { screen: '02-home', title: ['Every run, ', 'live', ''], tint: TEAL, sub: 'Loss, progress and GPU load of each run' },
  { screen: '03-running-run', title: ['Every metric, ', 'charted', ''], tint: TEAL, sub: 'Curves that move while the run trains' },
  { screen: '05-crashed-logs', title: ['Know when a run ', 'dies', ''], tint: AMBER, sub: 'Its state and the last lines of its log' },
  { screen: '06-samples', title: ['What the model ', 'makes', ''], tint: TEAL, sub: 'Sample images, as the run trains' },
  { screen: '07-alerts', title: ['', 'Alerts', ' from your runs'], tint: AMBER, sub: 'A NaN loss, a stalled run, a hot GPU' },
  { screen: '08-server', title: ['Who is on every ', 'GPU', ''], tint: TEAL, sub: 'Load and processes on each machine' },
]

/** The runyard mark (docs/assets/logo.svg): two lanes of a track, white outside, teal inside. */
function Mark({ size }: { size: number }) {
  return (
    <svg viewBox="0 0 256 256" width={size} height={size} aria-hidden>
      <path fill="#fafafa" d="M36 220 V144 A108 108 0 0 1 144 36 H220 L184 72 H144 A72 72 0 0 0 72 144 V220 Z" />
      <path fill={TEAL} d="M96 220 V144 A48 48 0 0 1 144 96 H220 L184 132 H144 A12 12 0 0 0 132 144 V220 Z" />
    </svg>
  )
}

export default function Store({ n }: { n: number }) {
  const { screen, title, tint, sub } = SHOTS[n]
  // The capture carries the phone's own status bar (its clock, a mute icon): black it out, the kit draws a clean one
  const wallpaper = `linear-gradient(#000, #000) top / 100% 50px no-repeat, url(${shots[`./screens/${screen}.png`]}) top / 100% auto no-repeat, #000`
  return (
    <Stage width={1320} height={2868} theme="dark" fidelity="realistic" className="ry-store">
      {() => (
        <>
          <div className="ry-lockup">
            <Mark size={76} />
            <span>runyard</span>
          </div>
          <div className="ry-caption">
            <h1>
              {title[0]}
              <span style={{ color: tint }}>{title[1]}</span>
              {title[2]}
            </h1>
            <p>{sub}</p>
          </div>
          <Phone width={1180} frame="silver" dark wallpaper={wallpaper} className="ry-phone" />
        </>
      )}
    </Stage>
  )
}
