/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{js,ts,jsx,tsx}"],
  theme: {
    extend: {
      screens: {
        tall: { raw: "(min-height: 800px)" },
        // => @media (min-height: 800px) { ... }
      },
    },
  },
  plugins: [require("@tailwindcss/forms")],
};
