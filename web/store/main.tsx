import { createRoot } from 'react-dom/client'
import Store from './Store'

// One screenshot per page: #1 to #6
createRoot(document.getElementById('store')!).render(<Store n={Number(location.hash.slice(1) || 1) - 1} />)
