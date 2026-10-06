import React from 'react';
import ReactDOM from 'react-dom/client';
import { LocaleProvider } from '@buildmax/gui';
import App from './App';
import './index.css';

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <LocaleProvider>
      <App />
    </LocaleProvider>
  </React.StrictMode>
);
