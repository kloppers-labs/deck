const horses = [
  {
    id: 1,
    name: "Mustang",
    image: "https://images.unsplash.com/photo-1509521715229-8d6716087f1e?w=400&h=500&fit=crop",
    breed: "Mustang",
    age: "5 years",
    location: "Texas, USA"
  },
  {
    id: 2,
    name: "Arabian",
    image: "https://images.unsplash.com/photo-1517665466334-b80f4eaba455?w=400&h=500&fit=crop",
    breed: "Arabian",
    age: "3 years",
    location: "California, USA"
  },
  {
    id: 3,
    name: "Thoroughbred",
    image: "https://images.unsplash.com/photo-1541854723890-f69103d6eb0e?w=400&h=500&fit=crop",
    breed: "Thoroughbred",
    age: "4 years",
    location: "Kentucky, USA"
  },
  {
    id: 4,
    name: "Quarter Horse",
    image: "https://images.unsplash.com/photo-1506430591311-c95d6bab5b5a?w=400&h=500&fit=crop",
    breed: "Quarter Horse",
    age: "6 years",
    location: "Oklahoma, USA"
  },
  {
    id: 5,
    name: "Fjord",
    image: "https://images.unsplash.com/photo-1568662350211-4e11b5c0e59c?w=400&h=500&fit=crop",
    breed: "Fjord",
    age: "7 years",
    location: "Colorado, USA"
  }
];

const cardsContainer = document.getElementById('cardsContainer');
const likeBtn = document.getElementById('likeBtn');
const passBtn = document.getElementById('passBtn');

let currentIndex = 0;

function createCard(horse) {
  const card = document.createElement('div');
  card.className = 'card';
  card.style.backgroundImage = `url('${horse.image}')`;
  card.dataset.id = horse.id;
  return card;
}

function showCard() {
  if (currentIndex >= horses.length) {
    cardsContainer.innerHTML = '<p style="text-align: center; padding: 50px; color: #6666;">No more horses!</p>';
    return;
  }
  
  const card = createCard(horses[currentIndex]);
  cardsInnerHTML = '';
  cardsContainer.appendChild(card);
  
  // Add swipe animation classes
  setTimeout(() => {
    card.classList.add('active');
  }, 10);
}

function swipe(direction) {
  const cards = document.querySelectorAll('.card');
  if (cards.length === 0) return;
  
  const card = cards[0];
  
  if (direction === 'like') {
    card.classList.add('like');
    card.addEventListener('animationend', () => {
      card.remove();
      currentIndex++;
      showCard();
    });
  } else {
    card.classList.add('pass');
    card.addEventListener('animationend', () => {
      card.remove();
      currentIndex++;
      showCard();
    });
  }
}

likeBtn.addEventListener('click', () => swipe('like'));
passBtn.addEventListener('click', () => swipe('pass'));

// Initial card
showCard();
</script>