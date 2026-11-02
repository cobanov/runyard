import { createRoot } from 'react-dom/client'
import Hero from './Hero'

// Mounts the hero into the website's #hero figure, in the page's own theme.
const dark = matchMedia('(prefers-color-scheme: dark)')
const root = createRoot(document.getElementById('hero')!)
const render = () => root.render(<Hero theme={dark.matches ? 'dark' : 'light'} />)
dark.addEventListener('change', render)
render()
